"""Abstract base class and shared types for git forge clients."""

from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from datetime import UTC, datetime
from enum import Enum

import httpx


class CIState(Enum):
    """Normalised CI status state."""

    PENDING = "pending"
    RUNNING = "running"
    SUCCESS = "success"
    FAILURE = "failure"
    CANCELLED = "cancelled"
    SKIPPED = "skipped"
    UNKNOWN = "unknown"


@dataclass
class ForgeInfo:
    """Parsed metadata from a git remote URL."""

    host: str          # e.g. "github.com", "gitlab.com", "git.example.com"
    slug: str          # e.g. "jbcom/radioactive-ralph"
    forge_type: str    # "github" | "gitlab" | "gitea"
    api_base_url: str  # e.g. "https://api.github.com"

    @property
    def owner(self) -> str:
        """Return the owner/org part of the slug.

        Returns:
            The owner string.
        """
        return self.slug.split("/")[0]

    @property
    def repo(self) -> str:
        """Return the repo name part of the slug.

        Returns:
            The repo name string.
        """
        return self.slug.split("/")[-1]


@dataclass
class ForgeCI:
    """Normalised CI status for a commit or PR head."""

    state: CIState
    details: list[dict[str, str]] = field(default_factory=list)

    @property
    def passed(self) -> bool:
        """Return True if CI state is success."""
        return self.state == CIState.SUCCESS

    @property
    def failed(self) -> bool:
        """Return True if CI state is failure or cancelled."""
        return self.state in (CIState.FAILURE, CIState.CANCELLED)


@dataclass
class ForgePR:
    """Normalised pull/merge request representation."""

    number: int
    title: str
    author: str
    branch: str
    head_sha: str
    is_draft: bool
    url: str
    updated_at: datetime
    review_approved: bool = False
    changes_requested: bool = False
    review_count: int = 0
    ci: ForgeCI | None = None

    @property
    def is_stale(self) -> bool:
        """Return True if PR has not been updated in 7 days."""
        delta = datetime.now(UTC) - self.updated_at
        return delta.days >= 7


@dataclass
class PRCreateParams:
    """Parameters for creating a pull request."""

    title: str
    body: str
    head: str          # source branch
    base: str          # target branch (default: main)
    draft: bool = False


class ForgeClient(ABC):
    """Abstract base class for git forge implementations.

    Args:
        info: Parsed forge metadata.
        http_client: Optional pre-configured httpx.AsyncClient.
    """

    def __init__(
        self,
        info: ForgeInfo,
        http_client: httpx.AsyncClient | None = None
    ) -> None:
        self.info = info
        self._external_client = http_client
        self._http: httpx.AsyncClient | None = http_client

    async def __aenter__(self) -> ForgeClient:
        await self._open()
        return self

    async def __aexit__(self, *_: object) -> None:
        await self._close()

    async def _open(self) -> None:
        """Called on context manager entry. Override to init HTTP client."""
        if self._http is None:
            self._http = self._make_http()

    async def _close(self) -> None:
        """Called on context manager exit. Override to close HTTP client."""
        if self._http and self._http is not self._external_client:
            await self._http.aclose()
            self._http = None

    @abstractmethod
    def _make_http(self) -> httpx.AsyncClient:
        """Create a new authenticated httpx.AsyncClient."""

    @abstractmethod
    async def list_prs(self, state: str = "open") -> list[ForgePR]:
        """List pull/merge requests for the repo."""

    @abstractmethod
    async def get_pr_ci(self, pr: ForgePR) -> ForgeCI:
        """Get CI status for a PR's head commit."""

    @abstractmethod
    async def get_pr_reviews(self, pr: ForgePR) -> ForgePR:
        """Populate review fields on pr (approved, changes_requested, review_count).."""

    @abstractmethod
    async def create_pr(self, params: PRCreateParams) -> ForgePR:
        """Create a new pull/merge request."""

    @abstractmethod
    async def merge_pr(self, pr: ForgePR) -> bool:
        """Squash-merge (or equivalent) a PR. Returns True on success."""

    @abstractmethod
    async def get_pr_diff(self, pr: ForgePR) -> str:
        """Fetch the unified diff for a pull/merge request."""
