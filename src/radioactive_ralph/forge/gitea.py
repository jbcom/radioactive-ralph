"""Gitea/Forgejo forge client — async httpx REST API.

Works with any self-hosted Gitea or Forgejo instance. The base URL is
derived from the ForgeInfo host (e.g. https://git.example.com).

Token resolution order:
  1. GITEA_TOKEN env var
  2. FORGEJO_TOKEN env var
  3. Raise AuthError with a helpful message

Note: Gitea and Forgejo share an identical REST API surface, so a single
implementation covers both.
"""

from __future__ import annotations

import logging
import os
from datetime import datetime
from typing import Any

import httpx

from .base import CIState, ForgeCI, ForgeClient, ForgeInfo, ForgePR, PRCreateParams

logger = logging.getLogger(__name__)


class AuthError(Exception):
    """Raised when no Gitea/Forgejo token can be found."""


def _discover_gitea_token() -> str:
    """Discover a Gitea/Forgejo token from the environment.

    Raises:
        AuthError: If no token is found in any expected location.
    """
    for var in ("GITEA_TOKEN", "FORGEJO_TOKEN"):
        if tok := os.environ.get(var):
            logger.debug("Gitea token from %s", var)
            return tok
    raise AuthError(
        "No Gitea/Forgejo token found. "
        "Set GITEA_TOKEN or FORGEJO_TOKEN."
    )


def _parse_status(status: str) -> CIState:
    mapping = {
        "success": CIState.SUCCESS,
        "failure": CIState.FAILURE,
        "error": CIState.FAILURE,
        "pending": CIState.PENDING,
        "running": CIState.RUNNING,
        "cancelled": CIState.CANCELLED,
        "skipped": CIState.SKIPPED,
    }
    return mapping.get(status, CIState.UNKNOWN)


class GiteaForge(ForgeClient):
    """Gitea/Forgejo implementation of ForgeClient.

    Gitea and Forgejo expose identical REST APIs, so this class handles both.
    Tested against Gitea 1.21+ and Forgejo 7.0+.

    Args:
        info: Parsed forge metadata (host, slug, forge_type).
        token: API token. If omitted, discovered from env vars.
    """

    def __init__(self, info: ForgeInfo, token: str | None = None) -> None:
        super().__init__(info)
        self._token = token or _discover_gitea_token()
        self._http: httpx.AsyncClient | None = None
        self._api_base = f"https://{info.host}/api/v1"

    def _make_http(self) -> httpx.AsyncClient:
        return httpx.AsyncClient(
            base_url=self._api_base,
            headers={"Authorization": f"token {self._token}"},
            timeout=30.0,
        )

    async def _open(self) -> None:
        self._http = self._make_http()

    async def _close(self) -> None:
        if self._http:
            await self._http.aclose()
            self._http = None

    def _c(self) -> httpx.AsyncClient:
        assert self._http, "Use as async context manager"
        return self._http

    async def _get(self, path: str, **params: Any) -> Any:
        resp = await self._c().get(path, params=params)
        resp.raise_for_status()
        return resp.json()

    async def _post(self, path: str, json: dict[str, Any]) -> Any:
        resp = await self._c().post(path, json=json)
        resp.raise_for_status()
        return resp.json()

    def _parse_pr(self, raw: dict[str, Any]) -> ForgePR:
        updated_raw = raw.get("updated_at", "")
        updated_at = (
            datetime.fromisoformat(updated_raw.replace("Z", "+00:00"))
            if updated_raw else datetime.now()
        )
        head = raw.get("head", {})
        return ForgePR(
            number=raw["number"],
            title=raw["title"],
            author=raw.get("user", {}).get("login", "unknown"),
            branch=head.get("label", head.get("ref", "")),
            head_sha=head.get("sha", ""),
            is_draft=raw.get("draft", False),
            url=raw.get("html_url", ""),
            updated_at=updated_at,
        )

    async def list_prs(self, state: str = "open") -> list[ForgePR]:
        """List pull requests for the repo.

        Args:
            state: Filter by state ("open", "closed", "all").

        Returns:
            List of normalised ForgePR objects.
        """
        raw = await self._get(
            f"/repos/{self.info.slug}/pulls",
            state=state, limit=50,
        )
        return [self._parse_pr(r) for r in raw]

    async def get_pr_ci(self, pr: ForgePR) -> ForgeCI:
        """Get CI status for the PR's head commit.

        Queries the Gitea commit statuses API, returning the aggregate state.

        Args:
            pr: The pull request to check.

        Returns:
            Normalised ForgeCI with aggregate state.
        """
        if not pr.head_sha:
            return ForgeCI(state=CIState.UNKNOWN)
        try:
            data = await self._get(
                f"/repos/{self.info.slug}/commits/{pr.head_sha}/statuses",
                limit=20,
            )
        except httpx.HTTPStatusError:
            return ForgeCI(state=CIState.UNKNOWN)

        if not data:
            return ForgeCI(state=CIState.UNKNOWN)

        states = [_parse_status(s.get("status", "")) for s in data]
        details = [{"name": s.get("context", ""), "state": st.value}
                   for s, st in zip(data, states, strict=False)]

        if any(s == CIState.FAILURE for s in states):
            return ForgeCI(state=CIState.FAILURE, details=details)
        if any(s in (CIState.PENDING, CIState.RUNNING) for s in states):
            return ForgeCI(state=CIState.PENDING, details=details)
        if all(s in (CIState.SUCCESS, CIState.SKIPPED) for s in states):
            return ForgeCI(state=CIState.SUCCESS, details=details)
        return ForgeCI(state=CIState.UNKNOWN, details=details)

    async def get_pr_reviews(self, pr: ForgePR) -> ForgePR:
        """Populate review fields on a PR.

        Args:
            pr: The pull request to annotate with review data.

        Returns:
            The same PR object with review fields populated.
        """
        try:
            reviews = await self._get(f"/repos/{self.info.slug}/pulls/{pr.number}/reviews")
            if isinstance(reviews, list):
                pr.review_count = len(reviews)
                pr.review_approved = any(r.get("state") == "APPROVED" for r in reviews)
                pr.changes_requested = any(
                    r.get("state") == "REQUEST_CHANGES" for r in reviews
                )
        except httpx.HTTPStatusError:
            pass
        return pr

    async def create_pr(self, params: PRCreateParams) -> ForgePR:
        """Create a pull request.

        Args:
            params: Title, body, head branch, base branch, and draft flag.

        Returns:
            The newly created PR as a ForgePR.
        """
        raw = await self._post(
            f"/repos/{self.info.slug}/pulls",
            json={
                "title": params.title,
                "body": params.body,
                "head": params.head,
                "base": params.base,
            },
        )
        return self._parse_pr(raw)

    async def _patch(self, path: str, json: dict[str, Any]) -> Any:
        resp = await self._c().patch(path, json=json)
        resp.raise_for_status()
        return resp.json()

    async def merge_pr(self, pr: ForgePR) -> bool:
        """Squash-merge a pull request and delete the source branch.

        Args:
            pr: The pull request to merge.

        Returns:
            True if merged successfully, False otherwise.
        """
        try:
            await self._post(
                f"/repos/{self.info.slug}/pulls/{pr.number}/merge",
                json={"Do": "squash", "delete_branch_after_merge": True},
            )
            return True
        except httpx.HTTPStatusError as e:
            logger.warning("Failed to merge #%d: HTTP %d", pr.number, e.response.status_code)
            return False

    async def close_pr(self, pr: ForgePR) -> bool:
        """Close a pull request without merging.

        Args:
            pr: The pull request to close.

        Returns:
            True if closed successfully.
        """
        try:
            await self._patch(
                f"/repos/{self.info.slug}/pulls/{pr.number}",
                json={"state": "closed"},
            )
            return True
        except httpx.HTTPStatusError as e:
            logger.warning("Failed to close #%d: HTTP %d", pr.number, e.response.status_code)
            return False

    async def add_comment(self, pr: ForgePR, body: str) -> None:
        """Post a comment on a pull request.

        Args:
            pr: The pull request to comment on.
            body: Markdown-formatted comment text.
        """
        await self._post(
            f"/repos/{self.info.slug}/issues/{pr.number}/comments",
            json={"body": body},
        )

    async def update_pr(
        self,
        pr: ForgePR,
        *,
        title: str | None = None,
        body: str | None = None,
        draft: bool | None = None,
    ) -> ForgePR:
        """Update PR metadata.

        Args:
            pr: The pull request to update.
            title: New title, or None to leave unchanged.
            body: New description, or None to leave unchanged.
            draft: New draft state, or None to leave unchanged.

        Returns:
            Updated ForgePR.
        """
        payload: dict[str, Any] = {}
        if title is not None:
            payload["title"] = title
        if body is not None:
            payload["body"] = body
        if payload:
            raw = await self._patch(
                f"/repos/{self.info.slug}/pulls/{pr.number}",
                json=payload,
            )
            return self._parse_pr(raw)
        return pr
