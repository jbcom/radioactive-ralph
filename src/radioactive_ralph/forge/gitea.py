"""Gitea/Forgejo forge client — async httpx REST API."""

from __future__ import annotations

import logging
import os
from datetime import UTC, datetime
from typing import Any

import httpx

from radioactive_ralph.forge.base import (
    CIState,
    ForgeCI,
    ForgeClient,
    ForgeInfo,
    ForgePR,
    PRCreateParams,
)

logger = logging.getLogger(__name__)


class AuthError(Exception):
    """Raised when no forge token can be found."""


def _discover_gitea_token() -> str:
    """Discover a Gitea/Forgejo token from the environment."""
    for var in ("GITEA_TOKEN", "FORGEJO_TOKEN"):
        if tok := os.environ.get(var):
            logger.debug("Gitea token from %s", var)
            return tok
    raise AuthError(
        "No Gitea/Forgejo token found. Set GITEA_TOKEN or FORGEJO_TOKEN."
    )


def _parse_status_state(status: str) -> CIState:
    """Map Gitea commit status to CIState."""
    mapping = {
        "success": CIState.SUCCESS,
        "failure": CIState.FAILURE,
        "error": CIState.FAILURE,
        "warning": CIState.FAILURE,
        "pending": CIState.PENDING,
        "running": CIState.RUNNING,
        "skipped": CIState.SKIPPED,
    }
    return mapping.get(status, CIState.UNKNOWN)


class GiteaForge(ForgeClient):
    """Gitea/Forgejo implementation of ForgeClient.

    Gitea and Forgejo expose identical REST APIs, so this class handles both.
    """

    def __init__(
        self,
        info: ForgeInfo,
        token: str | None = None,
        http_client: httpx.AsyncClient | None = None
    ) -> None:
        """Initialize the Gitea forge client.

        Args:
            info: Parsed forge metadata.
            token: Optional API token. If omitted, discovered via env vars.
            http_client: Optional pre-configured httpx.AsyncClient.
        """
        super().__init__(info, http_client=http_client)
        self._token = token or _discover_gitea_token()

    def _make_http(self) -> httpx.AsyncClient:
        """Create a new authenticated httpx.AsyncClient.

        Returns:
            The configured AsyncClient.
        """
        return httpx.AsyncClient(
            base_url=self.info.api_base_url,
            headers={"Authorization": f"token {self._token}"},
            timeout=30.0,
        )

    def _c(self) -> httpx.AsyncClient:
        """Return the internal HTTP client."""
        assert self._http, "Use as async context manager"
        return self._http

    async def _get(self, path: str, **params: Any) -> Any:
        """Perform a GET request."""
        resp = await self._c().get(path, params=params)
        resp.raise_for_status()
        return resp.json()

    async def _post(self, path: str, json: dict[str, Any]) -> Any:
        """Perform a POST request."""
        resp = await self._c().post(path, json=json)
        resp.raise_for_status()
        try:
            return resp.json()
        except ValueError:
            return {}

    def _parse_pr(self, raw: dict[str, Any]) -> ForgePR:
        """Parse raw Gitea API PR data into a ForgePR object."""
        updated_raw = raw.get("updated_at", "")
        updated_at = (
            datetime.fromisoformat(updated_raw.replace("Z", "+00:00"))
            if updated_raw else datetime.now(UTC)
        )
        head = raw.get("head", {})
        return ForgePR(
            number=int(raw["number"]),
            title=str(raw["title"]),
            author=str(raw.get("user", {}).get("login", "unknown")),
            branch=str(head.get("label", head.get("ref", ""))),
            head_sha=str(head.get("sha", "")),
            is_draft=bool(raw.get("draft", False)),
            url=str(raw.get("html_url", "")),
            updated_at=updated_at,
        )

    async def list_prs(self, state: str = "open") -> list[ForgePR]:
        """List pull requests for the repo."""
        raw = await self._get(
            f"/repos/{self.info.slug}/pulls",
            state=state, limit=50,
        )
        return [self._parse_pr(r) for r in raw]

    async def get_pr_ci(self, pr: ForgePR) -> ForgeCI:
        """Fetch the CI status for a specific PR's head commit."""
        if not pr.head_sha:
            return ForgeCI(state=CIState.UNKNOWN)
        try:
            data = await self._get(
                f"/repos/{self.info.slug}/statuses/{pr.head_sha}",
            )
        except httpx.HTTPError:
            return ForgeCI(state=CIState.UNKNOWN)

        if not data or not isinstance(data, list):
            return ForgeCI(state=CIState.UNKNOWN)

        states = [_parse_status_state(s.get("status", "")) for s in data]
        details = [
            {"name": str(s.get("context", "")), "state": st.value}
            for s, st in zip(data, states, strict=False)
        ]

        if any(s == CIState.FAILURE for s in states):
            return ForgeCI(state=CIState.FAILURE, details=details)
        if any(s in (CIState.PENDING, CIState.RUNNING) for s in states):
            return ForgeCI(state=CIState.PENDING, details=details)
        if all(s in (CIState.SUCCESS, CIState.SKIPPED) for s in states):
            return ForgeCI(state=CIState.SUCCESS, details=details)
        return ForgeCI(state=CIState.UNKNOWN, details=details)

    async def get_pr_reviews(self, pr: ForgePR) -> ForgePR:
        """Fetch and aggregate reviews for a Gitea pull request."""
        try:
            reviews = await self._get(f"/repos/{self.info.slug}/pulls/{pr.number}/reviews")
            if not isinstance(reviews, list):
                return pr

            # Group by reviewer and keep the latest state
            latest_reviews = {}
            for r in reviews:
                user_id = r.get("user", {}).get("id")
                if user_id:
                    # Gitea returns reviews in chronological order, so just overwrite
                    latest_reviews[user_id] = r.get("state")

            states = list(latest_reviews.values())
            pr.review_count = len(latest_reviews)
            pr.review_approved = any(s == "APPROVED" for s in states)
            pr.changes_requested = any(s == "REQUEST_CHANGES" for s in states)
        except httpx.HTTPError as e:
            logger.debug("Could not fetch reviews for #%d: %s", pr.number, e)

        return pr

    async def create_pr(self, params: PRCreateParams) -> ForgePR:
        """Create a pull request."""
        raw = await self._post(
            f"/repos/{self.info.slug}/pulls",
            json={
                "title": params.title,
                "body": params.body,
                "head": params.head,
                "base": params.base,
                "draft": params.draft,
            },
        )
        return self._parse_pr(raw)

    async def merge_pr(self, pr: ForgePR) -> bool:
        """Squash-merge a pull request and delete the source branch."""
        try:
            await self._post(
                f"/repos/{self.info.slug}/pulls/{pr.number}/merge",
                json={"Do": "squash", "delete_branch_after_merge": True},
            )
            return True
        except httpx.HTTPError as e:
            resp = getattr(e, "response", None)
            msg = resp.text if resp is not None else str(e)
            logger.warning("Failed to merge #%d: %s", pr.number, msg)
            return False

    async def get_pr_diff(self, pr: ForgePR) -> str:
        """Fetch the unified diff for a pull request using the .diff suffix."""
        async with httpx.AsyncClient(timeout=30) as client:
            # Gitea PRs provide a .diff view at their web URL
            headers = {"Authorization": f"token {self._token}"}
            resp = await client.get(f"{pr.url}.diff", headers=headers)
            resp.raise_for_status()
            return str(resp.text)
