"""GitLab forge client — async httpx REST API."""

from __future__ import annotations

import logging
import os
import subprocess
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


def _discover_gitlab_token() -> str:
    """Attempt to find a GitLab token from environment or glab CLI."""
    for var in ("GITLAB_TOKEN", "GL_TOKEN"):
        if tok := os.environ.get(var):
            logger.debug("GitLab token from %s", var)
            return tok
    try:
        result = subprocess.run(
            ["glab", "auth", "status", "--show-token"],
            capture_output=True, text=True, timeout=5,
        )
        if result.returncode == 0:
            import re
            if match := re.search(r"Token: (\S+)", result.stdout):
                logger.debug("GitLab token from `glab auth status`")
                return match.group(1)
    except (FileNotFoundError, subprocess.TimeoutExpired):
        pass
    raise AuthError(
        "No GitLab token found. "
        "Set GITLAB_TOKEN or GL_TOKEN, or run `glab auth login`."
    )


def _parse_pipeline_state(status: str) -> CIState:
    """Map GitLab pipeline status to CIState."""
    mapping = {
        "success": CIState.SUCCESS,
        "failed": CIState.FAILURE,
        "canceled": CIState.CANCELLED,
        "skipped": CIState.SKIPPED,
        "running": CIState.RUNNING,
        "pending": CIState.PENDING,
        "created": CIState.PENDING,
        "waiting_for_resource": CIState.PENDING,
    }
    return mapping.get(status, CIState.UNKNOWN)


class GitLabForge(ForgeClient):
    """GitLab implementation of ForgeClient.

    Works with gitlab.com and self-hosted instances.
    """

    def __init__(
        self,
        info: ForgeInfo,
        token: str | None = None,
        http_client: httpx.AsyncClient | None = None
    ) -> None:
        """Initialize the GitLab forge client.

        Args:
            info: Parsed forge metadata.
            token: Optional API token. If omitted, discovered via fallback chain.
            http_client: Optional pre-configured httpx.AsyncClient.
        """
        super().__init__(info, http_client=http_client)
        self._token = token or _discover_gitlab_token()
        # URL-encode the slug for API calls (org/repo → org%2Frepo)
        self._encoded_slug = self.info.slug.replace("/", "%2F")

    def _make_http(self) -> httpx.AsyncClient:
        """Create a new authenticated httpx.AsyncClient.

        Returns:
            The configured AsyncClient.
        """
        return httpx.AsyncClient(
            base_url=self.info.api_base_url,
            headers={"PRIVATE-TOKEN": self._token},
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
        return resp.json()

    async def _put(self, path: str, json: dict[str, Any]) -> Any:
        """Perform a PUT request."""
        resp = await self._c().put(path, json=json)
        resp.raise_for_status()
        return resp.json()

    def _parse_mr(self, raw: dict[str, Any]) -> ForgePR:
        """Parse raw GitLab MR data into a ForgePR object."""
        updated_raw = raw.get("updated_at", "")
        updated_at = (
            datetime.fromisoformat(updated_raw.replace("Z", "+00:00"))
            if updated_raw else datetime.now(UTC)
        )
        sha = str(raw.get("sha") or raw.get("diff_refs", {}).get("head_sha", ""))
        return ForgePR(
            number=int(raw["iid"]),  # GitLab uses iid for project-scoped MR numbers
            title=str(raw["title"]),
            author=str(raw.get("author", {}).get("username", "unknown")),
            branch=str(raw["source_branch"]),
            head_sha=sha,
            is_draft=bool(raw.get("draft", False) or raw.get("work_in_progress", False)),
            url=str(raw.get("web_url", "")),
            updated_at=updated_at,
        )

    async def list_prs(self, state: str = "open") -> list[ForgePR]:
        """List merge requests for the project."""
        # GitLab uses "opened" not "open"
        gl_state = "opened" if state == "open" else state
        raw = await self._get(
            f"/projects/{self._encoded_slug}/merge_requests",
            state=gl_state, per_page=100,
        )
        return [self._parse_mr(r) for r in raw]

    async def get_pr_ci(self, pr: ForgePR) -> ForgeCI:
        """Fetch the latest pipeline status for a merge request."""
        if not pr.head_sha:
            return ForgeCI(state=CIState.UNKNOWN)
        try:
            pipelines = await self._get(
                f"/projects/{self._encoded_slug}/pipelines",
                sha=pr.head_sha, per_page=5,
            )
        except httpx.HTTPError:
            return ForgeCI(state=CIState.UNKNOWN)

        if not pipelines or not isinstance(pipelines, list):
            return ForgeCI(state=CIState.UNKNOWN)

        latest = pipelines[0]
        state = _parse_pipeline_state(latest.get("status", ""))
        return ForgeCI(
            state=state,
            details=[{"name": "pipeline", "state": state.value, "id": str(latest.get("id", ""))}],
        )

    async def get_pr_reviews(self, pr: ForgePR) -> ForgePR:
        """Update a merge request with approval information from GitLab."""
        try:
            approvals = await self._get(
                f"/projects/{self._encoded_slug}/merge_requests/{pr.number}/approvals",
            )
            # approved_by is a list of users who approved
            approved_by = approvals.get("approved_by", [])
            pr.review_count = len(approved_by)
            pr.review_approved = bool(approvals.get("approved", False))
        except httpx.HTTPError as e:
            logger.debug("Could not fetch approvals for !%d: %s", pr.number, e)
        return pr

    async def create_pr(self, params: PRCreateParams) -> ForgePR:
        """Create a new merge request."""
        raw = await self._post(
            f"/projects/{self._encoded_slug}/merge_requests",
            json={
                "title": ("Draft: " if params.draft else "") + params.title,
                "description": params.body,
                "source_branch": params.head,
                "target_branch": params.base,
            },
        )
        return self._parse_mr(raw)

    async def merge_pr(self, pr: ForgePR) -> bool:
        """Squash-merge a merge request and remove the source branch."""
        try:
            await self._put(
                f"/projects/{self._encoded_slug}/merge_requests/{pr.number}/merge",
                json={"squash": True, "should_remove_source_branch": True},
            )
            return True
        except httpx.HTTPError as e:
            resp = getattr(e, "response", None)
            msg = resp.text if resp is not None else str(e)
            logger.warning("Failed to merge !%d: %s", pr.number, msg)
            return False

    async def get_pr_diff(self, pr: ForgePR) -> str:
        """Fetch the unified diff for a merge request using the .diff suffix."""
        async with httpx.AsyncClient(timeout=30) as client:
            # GitLab MRs provide a .diff view at their web URL
            resp = await client.get(f"{pr.url}.diff", headers={"PRIVATE-TOKEN": self._token})
            resp.raise_for_status()
            return str(resp.text)
