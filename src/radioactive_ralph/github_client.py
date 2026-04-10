"""GitHub API client — async httpx with token discovery fallback chain.

Token resolution order (matches gh CLI convention):
  1. GH_TOKEN env var      (gh CLI primary, CI-friendly)
  2. GITHUB_TOKEN env var  (GitHub Actions native)
  3. `gh auth token`       (developer machine with gh CLI installed)
  4. Raise AuthError with a clear message

Detection: if CLAUDECODE=1 env var is set, we're running inside a Claude Code
session and the GitHub MCP server may be available for interactive operations.
"""

from __future__ import annotations

import logging
import os
import re
import subprocess
from typing import Any

import httpx

logger = logging.getLogger(__name__)

GITHUB_API = "https://api.github.com"
GITHUB_API_VERSION = "2022-11-28"


class AuthError(Exception):
    """Raised when no GitHub token can be found."""


def inside_claude_code() -> bool:
    """Return True if we are running inside a Claude Code subprocess.

    Returns:
        True if the CLAUDECODE environment variable is set to 1, False otherwise.
    """
    return os.environ.get("CLAUDECODE") == "1"


def get_github_token() -> str:
    """Discover GitHub token via standard fallback chain.

    Returns:
        The discovered GitHub API token as a string.

    Raises:
        AuthError: If no GitHub token is found.
    """
    for var in ("GH_TOKEN", "GITHUB_TOKEN"):
        if tok := os.environ.get(var):
            logger.debug("GitHub token from %s env var", var)
            return tok

    try:
        result = subprocess.run(
            ["gh", "auth", "token"],
            capture_output=True,
            text=True,
            timeout=5,
        )
        if result.returncode == 0 and (tok := result.stdout.strip()):
            logger.debug("GitHub token from `gh auth token`")
            return tok
    except (FileNotFoundError, subprocess.TimeoutExpired):
        pass

    if inside_claude_code():
        hint = (
            "Running inside Claude Code — consider using the GitHub MCP server "
            "for interactive operations, or set GH_TOKEN / GITHUB_TOKEN."
        )
    else:
        hint = (
            "Set GH_TOKEN or GITHUB_TOKEN env var, "
            "or install and authenticate the gh CLI (`gh auth login`)."
        )

    raise AuthError(f"No GitHub token found. {hint}")


class GitHubClient:
    """Async GitHub REST API client backed by httpx."""

    def __init__(self, token: str | None = None) -> None:
        """Initialize the GitHub API client.

        Args:
            token: Optional GitHub Personal Access Token. If not provided, it will
                be discovered automatically.
        """
        self._token = token or get_github_token()
        self._client: httpx.AsyncClient | None = None

    def _make_client(self) -> httpx.AsyncClient:
        """Create and configure the underlying httpx.AsyncClient.

        Returns:
            A configured httpx.AsyncClient instance.
        """
        return httpx.AsyncClient(
            base_url=GITHUB_API,
            headers={
                "Authorization": f"Bearer {self._token}",
                "Accept": "application/vnd.github+json",
                "X-GitHub-Api-Version": GITHUB_API_VERSION,
            },
            timeout=30.0,
        )

    async def __aenter__(self) -> GitHubClient:
        self._client = self._make_client()
        return self

    async def __aexit__(self, *_: object) -> None:
        if self._client:
            await self._client.aclose()
            self._client = None

    async def get(self, path: str, **params: Any) -> Any:
        """GET request, returns parsed JSON.

        Args:
            path: The API endpoint path (e.g., '/repos/owner/repo/pulls').
            **params: Additional query parameters for the GET request.

        Returns:
            The parsed JSON response.
        """
        assert self._client, "Use as async context manager"
        resp = await self._client.get(path, params=params)
        resp.raise_for_status()
        return resp.json()

    async def get_paginated(self, path: str, **params: Any) -> list[Any]:
        """Perform a GET request and paginate through all results.

        Args:
            path: The API endpoint path.
            **params: Query parameters.

        Returns:
            A list containing all items from all pages.
        """
        assert self._client, "Use as async context manager"
        all_results = []
        url = path
        
        # Ensure per_page is set
        if "per_page" not in params:
            params["per_page"] = 100

        while url:
            resp = await self._client.get(url, params=params)
            resp.raise_for_status()
            
            data = resp.json()
            if isinstance(data, list):
                all_results.extend(data)
            elif isinstance(data, dict) and "items" in data:
                all_results.extend(data["items"])
            else:
                # Not a paginated list
                return data

            # Check Link header for next page
            url = None
            params = {}  # URL in link header already contains params
            if "link" in resp.headers:
                links = resp.headers["link"]
                match = re.search(r'<([^>]+)>;\s*rel="next"', links)
                if match:
                    url = match.group(1)
        
        return all_results

    async def post(self, path: str, json: dict[str, Any]) -> Any:
        """POST request, returns parsed JSON.

        Args:
            path: The API endpoint path.
            json: The JSON payload to send in the request body.

        Returns:
            The parsed JSON response.
        """
        assert self._client, "Use as async context manager"
        resp = await self._client.post(path, json=json)
        resp.raise_for_status()
        return resp.json()

    async def list_prs(self, repo: str, state: str = "open") -> list[dict[str, Any]]:
        """List pull requests for org/repo with pagination.

        Args:
            repo: The repository name in 'owner/repo' format.
            state: The state of the pull requests to filter by (default: 'open').

        Returns:
            A list of pull request dictionaries.
        """
        return await self.get_paginated(f"/repos/{repo}/pulls", state=state)

    async def get_pr_checks(self, repo: str, ref: str) -> list[dict[str, Any]]:
        """Get check runs for a commit ref with pagination.

        Args:
            repo: The repository name in 'owner/repo' format.
            ref: The commit SHA or reference.

        Returns:
            A list of check run dictionaries.
        """
        results = await self.get_paginated(f"/repos/{repo}/commits/{ref}/check-runs")
        # get_paginated already handles dict with "items", but check-runs is 
        # a special case where the list is in "check_runs"
        if isinstance(results, dict) and "check_runs" in results:
            return results["check_runs"]
        return results

    async def merge_pr(self, repo: str, pr_number: int) -> bool:
        """Squash-merge a PR. Returns True on success.

        Args:
            repo: The repository name in 'owner/repo' format.
            pr_number: The pull request number.

        Returns:
            True if the merge was successful, False otherwise.
        """
        try:
            await self.post(
                f"/repos/{repo}/pulls/{pr_number}/merge",
                json={"merge_method": "squash"},
            )
            return True
        except httpx.HTTPStatusError as e:
            logger.warning("Failed to merge %s #%d: %s", repo, pr_number, e.response.text)
            return False
