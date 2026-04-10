"""GitHub forge client — async httpx REST API.

Token resolution order (matches gh CLI convention):
  1. GH_TOKEN env var
  2. GITHUB_TOKEN env var
  3. `gh auth token` subprocess
  4. Raise AuthError with a helpful message
"""

from __future__ import annotations

import logging
import re
from datetime import UTC, datetime
from typing import Any

import httpx

from radioactive_ralph.forge.base import CIState, ForgeCI, ForgeClient, ForgeInfo, ForgePR, PRCreateParams
from radioactive_ralph.github_client import GITHUB_API_VERSION, AuthError, get_github_token

logger = logging.getLogger(__name__)


def _parse_commit_status(state: str) -> CIState:
    """Map GitHub commit status state to CIState.

    Args:
        state: The 'state' field from GitHub Commit Status API.

    Returns:
        The mapped CIState.
    """
    mapping = {
        "success": CIState.SUCCESS,
        "failure": CIState.FAILURE,
        "error": CIState.FAILURE,
        "pending": CIState.PENDING,
    }
    return mapping.get(state, CIState.UNKNOWN)


def _parse_ci_state(conclusion: str | None, status: str) -> CIState:
    """Map GitHub check run status/conclusion to CIState.

    Args:
        conclusion: The 'conclusion' field from GitHub API.
        status: The 'status' field from GitHub API.

    Returns:
        The mapped CIState.
    """
    if status not in ("completed",):
        return CIState.RUNNING if status == "in_progress" else CIState.PENDING
    mapping = {
        "success": CIState.SUCCESS,
        "failure": CIState.FAILURE,
        "cancelled": CIState.CANCELLED,
        "skipped": CIState.SKIPPED,
        "timed_out": CIState.FAILURE,
        "action_required": CIState.FAILURE,
    }
    return mapping.get(conclusion or "", CIState.UNKNOWN)


class GitHubForge(ForgeClient):
    """GitHub implementation of ForgeClient.

    Handles interactions with the GitHub REST API including listing PRs,
    fetching CI status, creating PRs, and merging.
    """

    def __init__(self, info: ForgeInfo, token: str | None = None) -> None:
        """Initialize the GitHub forge client.

        Args:
            info: Parsed forge metadata.
            token: Optional API token. If omitted, discovered via fallback chain.
        """
        super().__init__(info)
        self._token = token or get_github_token()
        self._http: httpx.AsyncClient | None = None

    def _make_http(self) -> httpx.AsyncClient:
        """Create a new authenticated httpx.AsyncClient.

        Returns:
            The configured AsyncClient.
        """
        return httpx.AsyncClient(
            base_url=self.info.api_base_url,
            headers={
                "Authorization": f"Bearer {self._token}",
                "Accept": "application/vnd.github+json",
                "X-GitHub-Api-Version": GITHUB_API_VERSION,
            },
            timeout=30.0,
        )

    async def _open(self) -> None:
        """Open the internal HTTP client.

        Returns:
            Description of return value.

        """
        self._http = self._make_http()

    async def _close(self) -> None:
        """Close the internal HTTP client.

        Returns:
            Description of return value.

        """
        if self._http:
            await self._http.aclose()
            self._http = None

    def _c(self) -> httpx.AsyncClient:
        """Return the internal HTTP client.

        Returns:
            The httpx.AsyncClient.

        Raises:
            AssertionError: If the client is not open.
        """
        assert self._http, "Use as async context manager"
        return self._http

    async def _get(self, path: str, **params: Any) -> Any:
        """Perform a GET request.

        Args:
            path: API path.
            **params: Query parameters.

        Returns:
            Parsed JSON response.
        """
        resp = await self._c().get(path, params=params)
        resp.raise_for_status()
        return resp.json()

    async def _get_paginated(self, path: str, **params: Any) -> list[Any]:
        """Perform a GET request and paginate through all results.

        Args:
            path: API path.
            **params: Query parameters.

        Returns:
            A list containing all items from all pages.
        """
        all_results = []
        url = path
        if "per_page" not in params:
            params["per_page"] = 100

        while url:
            resp = await self._c().get(url, params=params)
            resp.raise_for_status()

            data = resp.json()
            if isinstance(data, list):
                all_results.extend(data)
            elif isinstance(data, dict) and "items" in data:
                all_results.extend(data["items"])
            elif isinstance(data, dict) and "check_runs" in data:
                all_results.extend(data["check_runs"])
            else:
                return data

            # Check Link header for next page
            url = None
            params = {}  # URL in link header already contains params
            if "link" in resp.headers:
                links = resp.headers["link"]
                match = re.search(r'<([^>]+)>;\s*rel="next"', links)
                if match:
                    url = match.group(1).removeprefix(str(self.info.api_base_url))
        
        return all_results

    async def _post(self, path: str, json: dict[str, Any]) -> Any:
        """Perform a POST request.

        Args:
            path: API path.
            json: JSON body.

        Returns:
            Parsed JSON response.
        """
        resp = await self._c().post(path, json=json)
        resp.raise_for_status()
        return resp.json()

    def _parse_pr(self, raw: dict[str, Any]) -> ForgePR:
        """Parse raw GitHub PR JSON into a ForgePR object.

        Args:
            raw: Raw dictionary from the GitHub API.

        Returns:
            A populated ForgePR object.
        """
        updated_raw = raw.get("updated_at", "")
        updated_at = (
            datetime.fromisoformat(updated_raw.replace("Z", "+00:00"))
            if updated_raw else datetime.now(UTC)
        )
        return ForgePR(
            number=raw["number"],
            title=raw["title"],
            author=raw.get("user", {}).get("login", "unknown"),
            branch=raw["head"]["ref"],
            head_sha=raw["head"]["sha"],
            is_draft=raw.get("draft", False),
            url=raw.get("html_url", ""),
            updated_at=updated_at,
        )

    async def list_prs(self, state: str = "open") -> list[ForgePR]:
        """List pull requests for the repository with pagination.

        Args:
            state: The PR state (e.g., 'open', 'closed', 'all').

        Returns:
            A list of ForgePR objects.
        """
        raw = await self._get_paginated(
            f"/repos/{self.info.slug}/pulls",
            state=state,
        )
        return [self._parse_pr(r) for r in raw]

    async def get_pr_ci(self, pr: ForgePR) -> ForgeCI:
        """Fetch the CI status for a specific PR, combining Check Runs and Commit Statuses.

        Args:
            pr: The PR to check.

        Returns:
            A ForgeCI object representing the current CI state.
        """
        # 1. Fetch Check Runs with pagination
        runs = await self._get_paginated(
            f"/repos/{self.info.slug}/commits/{pr.head_sha}/check-runs",
        )
        
        # 2. Fetch Commit Statuses (legacy/simple CI) - status endpoint returns a combined summary
        status_data = await self._get(
            f"/repos/{self.info.slug}/commits/{pr.head_sha}/status",
        )
        statuses = status_data.get("statuses", [])

        if not runs and not statuses:
            return ForgeCI(state=CIState.UNKNOWN)

        all_details = []
        all_states = []

        for r in runs:
            state = _parse_ci_state(r.get("conclusion"), r.get("status", ""))
            all_states.append(state)
            all_details.append({"name": r.get("name", ""), "state": state.value})

        for s in statuses:
            state = _parse_commit_status(s.get("state", ""))
            all_states.append(state)
            all_details.append({"name": s.get("context", ""), "state": state.value})

        # Precedence: FAILURE > PENDING/RUNNING > SUCCESS/SKIPPED
        if any(s == CIState.FAILURE for s in all_states):
            return ForgeCI(state=CIState.FAILURE, details=all_details)
        if any(s in (CIState.PENDING, CIState.RUNNING) for s in all_states):
            return ForgeCI(state=CIState.PENDING, details=all_details)
        if all(s in (CIState.SUCCESS, CIState.SKIPPED) for s in all_states):
            return ForgeCI(state=CIState.SUCCESS, details=all_details)
        
        return ForgeCI(state=CIState.UNKNOWN, details=all_details)

    async def get_pr_reviews(self, pr: ForgePR) -> ForgePR:
        """Update a PR object with review status from GitHub.

        Considers only the latest review state for each unique reviewer.

        Args:
            pr: The PR object to update.

        Returns:
            The updated ForgePR object.
        """
        reviews = await self._get(f"/repos/{self.info.slug}/pulls/{pr.number}/reviews")
        if not isinstance(reviews, list):
            return pr

        # Group by reviewer and keep the latest state
        # GitHub returns reviews in chronological order by default, 
        # so later entries for the same user override earlier ones.
        latest_reviews = {}
        for r in reviews:
            user = r.get("user", {}).get("login")
            if user:
                latest_reviews[user] = r.get("state")

        states = list(latest_reviews.values())
        pr.review_count = len(latest_reviews)
        pr.changes_requested = any(s == "CHANGES_REQUESTED" for s in states)
        pr.review_approved = any(s == "APPROVED" for s in states)
        
        return pr

    async def create_pr(self, params: PRCreateParams) -> ForgePR:
        """Create a new pull request on GitHub.

        Args:
            params: Parameters for creating the PR.

        Returns:
            The created ForgePR object.
        """
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
        """Merge a pull request on GitHub using the squash method.

        Args:
            pr: The PR to merge.

        Returns:
            True if successful, False otherwise.
        """
        try:
            await self._post(
                f"/repos/{self.info.slug}/pulls/{pr.number}/merge",
                json={"merge_method": "squash"},
            )
            return True
        except httpx.HTTPError as e:
            resp = getattr(e, "response", None)
            msg = resp.text if resp is not None else str(e)
            logger.warning("Failed to merge #%d: %s", pr.number, msg)
            return False

    async def get_pr_diff(self, pr: ForgePR) -> str:
        """Fetch the unified diff of a pull request.

        Args:
            pr: The PR to fetch the diff for.

        Returns:
            The raw unified diff string.
        """
        headers = {
            "Authorization": f"Bearer {self._token}",
            "Accept": "application/vnd.github.v3.diff",
            "X-GitHub-Api-Version": GITHUB_API_VERSION,
        }
        async with httpx.AsyncClient(base_url=self.info.api_base_url, headers=headers, timeout=30) as client:
            resp = await client.get(f"/repos/{self.info.slug}/pulls/{pr.number}")
            resp.raise_for_status()
            return resp.text
