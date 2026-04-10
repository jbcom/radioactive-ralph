"""GitHub API client — async httpx with token discovery fallback chain."""

from __future__ import annotations

import logging
import os
import re
import subprocess
from typing import Any, cast

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
    """Discover GitHub token via standard fallback chain."""
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

    def _c(self) -> httpx.AsyncClient:
        assert self._client, "Use as async context manager"
        return self._client

    async def get(self, path: str, **params: Any) -> Any:
        resp = await self._c().get(path, params=params)
        resp.raise_for_status()
        return resp.json()

    async def get_paginated(self, path: str, **params: Any) -> list[Any]:
        all_results = []
        url: str | None = path

        if "per_page" not in params:
            params["per_page"] = 100

        while url:
            resp = await self._c().get(url, params=params if not url.startswith("http") else None)
            resp.raise_for_status()

            data = resp.json()
            if isinstance(data, list):
                all_results.extend(data)
            elif isinstance(data, dict) and "items" in data:
                all_results.extend(data["items"])
            else:
                return [data] if not isinstance(data, list) else data

            next_url = None
            if "link" in resp.headers:
                links = str(resp.headers["link"])
                match = re.search(r'<([^>]+)>;\s*rel="next"', links)
                if match:
                    next_url = match.group(1)
            url = next_url

        return all_results

    async def post(self, path: str, json: dict[str, Any]) -> Any:
        resp = await self._c().post(path, json=json)
        resp.raise_for_status()
        return resp.json()

    async def list_prs(self, repo: str, state: str = "open") -> list[dict[str, Any]]:
        return cast(
            list[dict[str, Any]],
            await self.get_paginated(f"/repos/{repo}/pulls", state=state)
        )

    async def get_pr_checks(self, repo: str, ref: str) -> list[dict[str, Any]]:
        results = await self.get_paginated(f"/repos/{repo}/commits/{ref}/check-runs")
        # get_paginated logic might need adjustment if check-runs is nested
        return cast(list[dict[str, Any]], results)

    async def merge_pr(self, repo: str, pr_number: int) -> bool:
        try:
            await self.post(
                f"/repos/{repo}/pulls/{pr_number}/merge",
                json={"merge_method": "squash"},
            )
            return True
        except httpx.HTTPError as e:
            logger.warning("Failed to merge %s #%d: %s", repo, pr_number, e)
            return False
