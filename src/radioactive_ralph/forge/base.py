"""Abstract base classes for forge (git hosting) clients.

A ForgeClient abstracts over GitHub, GitLab, Gitea, and Forgejo so that
radioactive-ralph works with any hosted or self-hosted git forge.

All methods are async. Implementations use httpx.AsyncClient internally
and should be used as async context managers.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from datetime import UTC, datetime
from enum import Enum


class CIState(str, Enum):
    """Normalised CI state across forges."""

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

    @property
    def owner(self) -> str:
        return self.slug.split("/")[0]

    @property
    def repo(self) -> str:
        return self.slug.split("/")[-1]


@dataclass
class ForgeCI:
    """Normalised CI status for a commit or PR head."""

    state: CIState
    details: list[dict[str, str]] = field(default_factory=list)

    @property
    def passed(self) -> bool:
        return self.state == CIState.SUCCESS

    @property
    def failed(self) -> bool:
        return self.state in (CIState.FAILURE, CIState.CANCELLED)

    @property
    def pending(self) -> bool:
        return self.state in (CIState.PENDING, CIState.RUNNING)


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
    """Abstract forge client — implement for each hosting platform."""

    def __init__(self, info: ForgeInfo) -> None:
        self.info = info

    async def __aenter__(self) -> ForgeClient:
        await self._open()
        return self

    async def __aexit__(self, *_: object) -> None:
        await self._close()

    async def _open(self) -> None:
        """Called on context manager entry. Override to init HTTP client."""
        return

    async def _close(self) -> None:
        """Called on context manager exit. Override to close HTTP client."""
        return

    @abstractmethod
    async def list_prs(self, state: str = "open") -> list[ForgePR]:
        """List pull/merge requests for the repo."""

    @abstractmethod
    async def get_pr_ci(self, pr: ForgePR) -> ForgeCI:
        """Get CI status for a PR's head commit."""

    @abstractmethod
    async def get_pr_reviews(self, pr: ForgePR) -> ForgePR:
        """Populate review fields on pr (approved, changes_requested, review_count)."""

    @abstractmethod
    async def create_pr(self, params: PRCreateParams) -> ForgePR:
        """Create a new pull/merge request."""

    @abstractmethod
    async def merge_pr(self, pr: ForgePR) -> bool:
        """Squash-merge (or equivalent) a PR. Returns True on success."""
