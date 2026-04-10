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
import subprocess
from typing import Any

import httpx

logger = logging.getLogger(__name__)

GITHUB_API = "https://api.github.com"
GITHUB_API_VERSION = "2022-11-28"


class AuthError(Exception):
    """Raised when no GitHub token can be found."""


def inside_claude_code() -> bool:
    """Return True if we are running inside a Claude Code subprocess."""
    return os.environ.get("CLAUDECODE") == "1"


def get_github_token() -> str:
    """Discover GitHub token via standard fallback chain.

    Raises AuthError if no token is found.
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
        self._token = token or get_github_token()
        self._client: httpx.AsyncClient | None = None

    def _make_client(self) -> httpx.AsyncClient:
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
        """GET request, returns parsed JSON."""
        assert self._client, "Use as async context manager"
        resp = await self._client.get(path, params=params)
        resp.raise_for_status()
        return resp.json()

    async def post(self, path: str, json: dict[str, Any]) -> Any:
        """POST request, returns parsed JSON."""
        assert self._client, "Use as async context manager"
        resp = await self._client.post(path, json=json)
        resp.raise_for_status()
        return resp.json()

    async def list_prs(self, repo: str, state: str = "open") -> list[dict[str, Any]]:
        """List pull requests for org/repo."""
        return await self.get(f"/repos/{repo}/pulls", state=state, per_page=100)  # type: ignore[return-value]

    async def get_pr_checks(self, repo: str, ref: str) -> list[dict[str, Any]]:
        """Get check runs for a commit ref."""
        data = await self.get(f"/repos/{repo}/commits/{ref}/check-runs", per_page=100)
        return data.get("check_runs", [])  # type: ignore[return-value]

    async def merge_pr(self, repo: str, pr_number: int) -> bool:
        """Squash-merge a PR. Returns True on success."""
        try:
            await self.post(
                f"/repos/{repo}/pulls/{pr_number}/merge",
                json={"merge_method": "squash"},
            )
            return True
        except httpx.HTTPStatusError as e:
            logger.warning("Failed to merge %s #%d: %s", repo, pr_number, e.response.text)
            return False
