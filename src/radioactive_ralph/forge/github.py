"""GitHub forge client — async httpx REST API.

Token resolution order (matches gh CLI convention):
  1. GH_TOKEN env var
  2. GITHUB_TOKEN env var
  3. `gh auth token` subprocess
  4. Raise AuthError with a helpful message
"""

from __future__ import annotations

import logging
import os
import subprocess
from datetime import datetime
from typing import Any

import httpx

from .base import CIState, ForgeCI, ForgeClient, ForgeInfo, ForgePR, PRCreateParams

logger = logging.getLogger(__name__)

GITHUB_API = "https://api.github.com"
GITHUB_API_VERSION = "2022-11-28"


class AuthError(Exception):
    """Raised when no forge token can be found."""


def _discover_github_token() -> str:
    for var in ("GH_TOKEN", "GITHUB_TOKEN"):
        if tok := os.environ.get(var):
            logger.debug("GitHub token from %s", var)
            return tok
    try:
        result = subprocess.run(
            ["gh", "auth", "token"],
            capture_output=True, text=True, timeout=5,
        )
        if result.returncode == 0 and (tok := result.stdout.strip()):
            logger.debug("GitHub token from `gh auth token`")
            return tok
    except (FileNotFoundError, subprocess.TimeoutExpired):
        pass
    raise AuthError(
        "No GitHub token found. "
        "Set GH_TOKEN or GITHUB_TOKEN, or run `gh auth login`."
    )


def _parse_ci_state(conclusion: str | None, status: str) -> CIState:
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
    """GitHub implementation of ForgeClient."""

    def __init__(self, info: ForgeInfo, token: str | None = None) -> None:
        super().__init__(info)
        self._token = token or _discover_github_token()
        self._http: httpx.AsyncClient | None = None

    def _make_http(self) -> httpx.AsyncClient:
        return httpx.AsyncClient(
            base_url=GITHUB_API,
            headers={
                "Authorization": f"Bearer {self._token}",
                "Accept": "application/vnd.github+json",
                "X-GitHub-Api-Version": GITHUB_API_VERSION,
            },
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
        raw = await self._get(
            f"/repos/{self.info.slug}/pulls",
            state=state, per_page=100,
        )
        return [self._parse_pr(r) for r in raw]

    async def get_pr_ci(self, pr: ForgePR) -> ForgeCI:
        data = await self._get(
            f"/repos/{self.info.slug}/commits/{pr.head_sha}/check-runs",
            per_page=100,
        )
        runs = data.get("check_runs", [])
        if not runs:
            return ForgeCI(state=CIState.UNKNOWN)

        states = [_parse_ci_state(r.get("conclusion"), r.get("status", "")) for r in runs]
        details = [
            {"name": r.get("name", ""), "state": s.value}
            for r, s in zip(runs, states, strict=False)
        ]

        if any(s == CIState.FAILURE for s in states):
            return ForgeCI(state=CIState.FAILURE, details=details)
        if any(s in (CIState.PENDING, CIState.RUNNING) for s in states):
            return ForgeCI(state=CIState.PENDING, details=details)
        if all(s in (CIState.SUCCESS, CIState.SKIPPED) for s in states):
            return ForgeCI(state=CIState.SUCCESS, details=details)
        return ForgeCI(state=CIState.UNKNOWN, details=details)

    async def get_pr_reviews(self, pr: ForgePR) -> ForgePR:
        reviews = await self._get(f"/repos/{self.info.slug}/pulls/{pr.number}/reviews")
        if isinstance(reviews, list):
            pr.review_count = len(reviews)
            pr.changes_requested = any(r.get("state") == "CHANGES_REQUESTED" for r in reviews)
            pr.review_approved = any(r.get("state") == "APPROVED" for r in reviews)
        return pr

    async def create_pr(self, params: PRCreateParams) -> ForgePR:
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
        try:
            await self._post(
                f"/repos/{self.info.slug}/pulls/{pr.number}/merge",
                json={"merge_method": "squash"},
            )
            return True
        except httpx.HTTPStatusError as e:
            logger.warning("Failed to merge #%d: %s", pr.number, e.response.text)
            return False
