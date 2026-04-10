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
    api_base_url: str  # e.g. "https://api.github.com" or "https://git.example.com/api/v1"

    @property
    def owner(self) -> str:
        """Return the owner/org part of the slug.

        Returns:
            The owner/org string.
        """
        return self.slug.split("/")[0]

    @property
    def repo(self) -> str:
        """Return the repository name part of the slug.

        Returns:
            The repository name string.
        """
        return self.slug.split("/")[-1]


@dataclass
class ForgeCI:
    """Normalised CI status for a commit or PR head."""

    state: CIState
    details: list[dict[str, str]] = field(default_factory=list)

    @property
    def passed(self) -> bool:
        """Check if all CI jobs passed successfully.

        Returns:
            True if state is SUCCESS, False otherwise.
        """
        return self.state == CIState.SUCCESS

    @property
    def failed(self) -> bool:
        """Check if any CI jobs failed or were cancelled.

        Returns:
            True if state is FAILURE or CANCELLED, False otherwise.
        """
        return self.state in (CIState.FAILURE, CIState.CANCELLED)

    @property
    def pending(self) -> bool:
        """Check if CI jobs are still pending or running.

        Returns:
            True if state is PENDING or RUNNING, False otherwise.
        """
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
        """Check if the pull request has not been updated in the last 7 days.

        Returns:
            True if the PR is stale, False otherwise.
        """
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
        """Initialize the ForgeClient.

        Args:
            info: The ForgeInfo object containing repository and API details.
        """
        self.info = info

    async def __aenter__(self) -> ForgeClient:
        await self._open()
        return self

    async def __aexit__(self, *_: object) -> None:
        await self._close()

    async def _open(self) -> None:
        """Called on context manager entry. Override to init HTTP client.

        Returns:
            None.
        """
        return

    async def _close(self) -> None:
        """Called on context manager exit. Override to close HTTP client.

        Returns:
            None.
        """
        return

    @abstractmethod
    async def list_prs(self, state: str = "open") -> list[ForgePR]:
        """List pull/merge requests for the repo.

        Args:
            state: Filter by state (e.g. "open", "closed", "all").

        Returns:
            A list of ForgePR objects.
        """

    @abstractmethod
    async def get_pr_ci(self, pr: ForgePR) -> ForgeCI:
        """Get CI status for a PR's head commit.

        Args:
            pr: The pull/merge request to check.

        Returns:
            A ForgeCI object containing the aggregated CI state.
        """

    @abstractmethod
    async def get_pr_reviews(self, pr: ForgePR) -> ForgePR:
        """Populate review fields on pr.

        Args:
            pr: The pull/merge request to update.

        Returns:
            The same ForgePR object with review_approved, changes_requested,
            and review_count fields updated.
        """

    @abstractmethod
    async def create_pr(self, params: PRCreateParams) -> ForgePR:
        """Create a new pull/merge request.

        Args:
            params: Parameters for the new PR (title, body, etc.).

        Returns:
            The newly created ForgePR object.
        """

    @abstractmethod
    async def merge_pr(self, pr: ForgePR) -> bool:
        """Squash-merge (or equivalent) a PR.

        Args:
            pr: The pull/merge request to merge.

        Returns:
            True if merged successfully, False otherwise.
        """

    @abstractmethod
    async def get_pr_diff(self, pr: ForgePR) -> str:
        """Fetch the unified diff for a pull/merge request.

        Args:
            pr: The pull/merge request to fetch the diff for.

        Returns:
            The raw unified diff string.
        """
