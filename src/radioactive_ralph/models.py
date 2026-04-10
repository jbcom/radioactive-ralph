"""Domain models for the orchestrator."""

from __future__ import annotations

from datetime import UTC, datetime
from enum import Enum
from pathlib import Path

from pydantic import BaseModel, Field


class WorkPriority(Enum):
    """Priority level for a work item."""

    LOW = 0
    MEDIUM = 1
    HIGH = 2
    URGENT = 3


class PRStatus(Enum):
    """Internal status of a pull request."""

    NEEDS_REVIEW = "needs_review"
    CHANGES_REQUESTED = "changes_requested"
    MERGE_READY = "merge_ready"
    STALE = "stale"
    UNKNOWN = "unknown"


class PRInfo(BaseModel):
    """Aggregated information about a pull request."""

    repo: str
    number: int
    title: str
    author: str
    branch: str
    url: str
    status: PRStatus
    updated_at: datetime
    ci_passed: bool = False
    is_draft: bool = False
    review_count: int = 0

    @property
    def is_mergeable(self) -> bool:
        """Return True if the PR is approved, passed CI, and not a draft.

        Returns:
            Boolean mergeability status.
        """
        return self.status == PRStatus.MERGE_READY and self.ci_passed and not self.is_draft


class ReviewSeverity(Enum):
    """Severity of a review finding."""

    ERROR = "error"
    WARNING = "warning"
    SUGGESTION = "suggestion"
    NITPICK = "nitpick"


class ReviewFinding(BaseModel):
    """A single finding from an AI code review."""

    severity: ReviewSeverity
    file: str
    line: int | None = None
    issue: str
    fix: str = ""


class ReviewResult(BaseModel):
    """Full result of an AI code review."""

    pr: PRInfo
    findings: list[ReviewFinding] = Field(default_factory=list)
    approved: bool
    summary: str

    @property
    def has_blocking_issues(self) -> bool:
        """Return True if any findings are errors.

        Returns:
            Boolean blocking status.
        """
        return any(f.severity == ReviewSeverity.ERROR for f in self.findings)


class WorkItem(BaseModel):
    """A unit of work discovered in a repository."""

    id: str
    repo_path: str
    description: str
    priority: WorkPriority
    source: str = ""
    context: str = ""
    timeout_seconds: int = 1800
    created_at: datetime = Field(default_factory=lambda: datetime.now(UTC))

    @property
    def repo_name(self) -> str:
        """Return the name of the repository from its path.

        Returns:
            The directory name of the repo.
        """
        return Path(self.repo_path).name


class AgentRun(BaseModel):
    """Tracking data for a running Claude Code agent."""

    item: WorkItem
    process_id: int
    started_at: datetime
    completed_at: datetime | None = None
    exit_code: int | None = None
    output: str = ""

    @property
    def is_active(self) -> bool:
        """Return True if the agent process is still running.

        Returns:
            Boolean activity status.
        """
        return self.completed_at is None


class OrchestratorState(BaseModel):
    """Persistent state of the orchestrator daemon."""

    cycle_count: int = 0
    last_scan: datetime | None = None
    last_discovery: datetime | None = None
    work_queue: list[WorkItem] = Field(default_factory=list)
    active_runs: list[AgentRun] = Field(default_factory=list)
