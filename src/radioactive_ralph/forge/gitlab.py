"""GitLab forge client — async httpx REST API.

Token resolution order:
  1. GITLAB_TOKEN env var
  2. GL_TOKEN env var
  3. `glab auth status --show-token` subprocess
  4. Raise AuthError with a helpful message

Self-hosted GitLab instances are supported: the base URL is derived from
the ForgeInfo host (e.g. https://gitlab.example.com/api/v4).
"""

from __future__ import annotations

import logging
import os
import subprocess
from datetime import datetime
from typing import Any

import httpx

from radioactive_ralph.forge.base import CIState, ForgeCI, ForgeClient, ForgeInfo, ForgePR, PRCreateParams

logger = logging.getLogger(__name__)


class AuthError(Exception):
    """Raised when no GitLab token can be found."""


def _discover_gitlab_token() -> str:
    """Discover GitLab API token via standard fallback chain.

    Returns:
        The discovered token string.

    Raises:
        AuthError: If no GitLab token is found.
    """
    for var in ("GITLAB_TOKEN", "GL_TOKEN", "CI_JOB_TOKEN"):
        if tok := os.environ.get(var):
            logger.debug("GitLab token from %s", var)
            return tok
    try:
        result = subprocess.run(
            ["glab", "auth", "status", "--show-token"],
            capture_output=True, text=True, timeout=5,
        )
        if result.returncode == 0:
            for line in result.stdout.splitlines():
                if "Token:" in line:
                    tok = line.split("Token:")[-1].strip()
                    if tok:
                        return tok
    except (FileNotFoundError, subprocess.TimeoutExpired):
        pass
    raise AuthError(
        "No GitLab token found. "
        "Set GITLAB_TOKEN or GL_TOKEN, or run `glab auth login`."
    )


def _parse_pipeline_state(status: str) -> CIState:
    """Parse GitLab pipeline status into a normalised CIState.

    Args:
        status: The raw status string from GitLab.

    Returns:
        The corresponding CIState enum value.
    """
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

    def __init__(self, info: ForgeInfo, token: str | None = None) -> None:
        """Initialize the GitLab forge client.

        Args:
            info: The ForgeInfo object containing repository details.
            token: Optional GitLab API token.
        """
        super().__init__(info)
        self._token = token or _discover_gitlab_token()
        self._http: httpx.AsyncClient | None = None
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

    async def _open(self) -> None:
        """Called on context manager entry. Initializes the HTTP client.

        Returns:
            None.
        """
        self._http = self._make_http()

    async def _close(self) -> None:
        """Called on context manager exit. Closes the HTTP client.

        Returns:
            None.
        """
        if self._http:
            await self._http.aclose()
            self._http = None

    def _c(self) -> httpx.AsyncClient:
        """Get the initialized HTTP client.

        Returns:
            The underlying httpx.AsyncClient.
        """
        assert self._http, "Use as async context manager"
        return self._http

    async def _get(self, path: str, **params: Any) -> Any:
        """Make a GET request to the GitLab API.

        Args:
            path: The API endpoint path.
            **params: Additional query parameters.

        Returns:
            The parsed JSON response.
        """
        resp = await self._c().get(path, params=params)
        resp.raise_for_status()
        return resp.json()

    async def _post(self, path: str, json: dict[str, Any]) -> Any:
        """Make a POST request to the GitLab API.

        Args:
            path: The API endpoint path.
            json: The JSON payload.

        Returns:
            The parsed JSON response.
        """
        resp = await self._c().post(path, json=json)
        resp.raise_for_status()
        return resp.json()

    async def _put(self, path: str, json: dict[str, Any]) -> Any:
        """Make a PUT request to the GitLab API.

        Args:
            path: The API endpoint path.
            json: The JSON payload.

        Returns:
            The parsed JSON response.
        """
        resp = await self._c().put(path, json=json)
        resp.raise_for_status()
        return resp.json()

    def _parse_mr(self, raw: dict[str, Any]) -> ForgePR:
        """Parse a raw GitLab merge request response into a ForgePR.

        Args:
            raw: The raw dictionary representation of the MR.

        Returns:
            A normalized ForgePR object.
        """
        updated_raw = raw.get("updated_at", "")
        updated_at = (
            datetime.fromisoformat(updated_raw.replace("Z", "+00:00"))
            if updated_raw else datetime.now()
        )
        sha = raw.get("sha") or raw.get("diff_refs", {}).get("head_sha", "")
        return ForgePR(
            number=raw["iid"],  # GitLab uses iid for project-scoped MR numbers
            title=raw["title"],
            author=raw.get("author", {}).get("username", "unknown"),
            branch=raw["source_branch"],
            head_sha=sha,
            is_draft=raw.get("draft", False) or raw["title"].startswith(("Draft:", "WIP:")),
            url=raw.get("web_url", ""),
            updated_at=updated_at,
        )

    async def list_prs(self, state: str = "open") -> list[ForgePR]:
        """List merge requests for the repository.

        Args:
            state: The state of the MRs to fetch (default: "open").

        Returns:
            A list of ForgePR objects.
        """
        # GitLab uses "opened" not "open"
        gl_state = "opened" if state == "open" else state
        raw = await self._get(
            f"/projects/{self._encoded_slug}/merge_requests",
            state=gl_state, per_page=100,
        )
        return [self._parse_mr(r) for r in raw]

    async def get_pr_ci(self, pr: ForgePR) -> ForgeCI:
        """Fetch the CI state for a merge request.

        Args:
            pr: The pull/merge request.

        Returns:
            A ForgeCI object indicating the CI state.
        """
        if not pr.head_sha:
            return ForgeCI(state=CIState.UNKNOWN)
        try:
            pipelines = await self._get(
                f"/projects/{self._encoded_slug}/pipelines",
                sha=pr.head_sha, per_page=5,
            )
        except httpx.HTTPStatusError:
            return ForgeCI(state=CIState.UNKNOWN)

        if not pipelines:
            return ForgeCI(state=CIState.UNKNOWN)

        latest = pipelines[0]
        state = _parse_pipeline_state(latest.get("status", ""))
        return ForgeCI(
            state=state,
            details=[{"name": "pipeline", "state": state.value, "id": str(latest.get("id", ""))}],
        )

    async def get_pr_reviews(self, pr: ForgePR) -> ForgePR:
        """Update a merge request with approval information from GitLab.

        Args:
            pr: The merge request to update.

        Returns:
            The updated ForgePR object.
        """
        try:
            approvals = await self._get(
                f"/projects/{self._encoded_slug}/merge_requests/{pr.number}/approvals",
            )
            # approved_by is a list of users who approved
            approved_by = approvals.get("approved_by", [])
            pr.review_count = len(approved_by)
            pr.review_approved = approvals.get("approved", False)
            # GitLab doesn't have a direct "changes requested" state in the same way,
            # but we could potentially check for negative review comments if needed.
        except httpx.HTTPStatusError as e:
            logger.debug("Could not fetch approvals for !%d: %s", pr.number, e)
        return pr

    async def create_pr(self, params: PRCreateParams) -> ForgePR:
        """Create a new merge request.

        Args:
            params: The parameters for the new PR.

        Returns:
            The newly created ForgePR object.
        """
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
        """Merge a pull/merge request.

        Args:
            pr: The pull/merge request to merge.

        Returns:
            True if the merge was successful, False otherwise.
        """
        try:
            await self._put(
                f"/projects/{self._encoded_slug}/merge_requests/{pr.number}/merge",
                json={"squash": True, "should_remove_source_branch": True},
            )
            return True
        except httpx.HTTPStatusError as e:
            logger.warning("Failed to merge !%d: %s", pr.number, e.response.text)
            return False
