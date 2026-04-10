"""Pydantic models and settings for radioactive_ralph state, work items, and agent runs.

The :class:`AutoloopConfig` is a :class:`pydantic_settings.BaseSettings` subclass,
so every field can be overridden via environment variable (prefix: ``RALPH_``) or
TOML config file, without any code changes.

Example env-var overrides::

    RALPH_MAX_PARALLEL_AGENTS=10 ralph run
    RALPH_CYCLE_BACKOFF_BASE_S=60 ralph run
    RALPH_BULK_MODEL=claude-haiku-4-5-20251001 ralph run
"""

from __future__ import annotations

from datetime import UTC, datetime
from enum import Enum
from pathlib import Path

from pydantic import BaseModel, Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class PRStatus(str, Enum):
    """Classification of a pull request's current state."""

    MERGE_READY = "merge_ready"
    NEEDS_REVIEW = "needs_review"
    NEEDS_FIXES = "needs_fixes"
    CI_FAILING = "ci_failing"
    IN_PROGRESS = "in_progress"
    STALE = "stale"
    DRAFT = "draft"


class ReviewSeverity(str, Enum):
    """Severity level for a review finding."""

    ERROR = "error"
    WARNING = "warning"
    SUGGESTION = "suggestion"
    NITPICK = "nitpick"


class WorkPriority(int, Enum):
    """Priority levels for work items. Lower number = higher priority."""

    CI_FAILURE = 1
    PR_FIXES = 2
    DOC_SWEEP = 3
    MISSING_FILES = 4
    STATE_NEXT = 5
    DESIGN_FEATURE = 6
    POLISH = 7


class PRInfo(BaseModel):
    """Representation of a GitHub pull request."""

    repo: str
    number: int
    title: str
    author: str
    branch: str
    status: PRStatus
    ci_passed: bool = False
    review_count: int = 0
    has_unresolved_comments: bool = False
    is_draft: bool = False
    updated_at: datetime = Field(default_factory=lambda: datetime.now(UTC))
    url: str = ""

    @property
    def is_mergeable(self) -> bool:
        return self.status == PRStatus.MERGE_READY and self.ci_passed and not self.is_draft


class ReviewFinding(BaseModel):
    """A single finding from a code review."""

    severity: ReviewSeverity
    file: str
    line: int | None = None
    issue: str
    fix: str


class ReviewResult(BaseModel):
    """Result of an AI code review."""

    pr: PRInfo
    findings: list[ReviewFinding] = Field(default_factory=list)
    approved: bool = False
    summary: str = ""

    @property
    def has_blocking_issues(self) -> bool:
        return any(f.severity == ReviewSeverity.ERROR for f in self.findings)


class WorkItem(BaseModel):
    """A unit of work to be executed by an agent."""

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
        return Path(self.repo_path).name


class AgentResult(BaseModel):
    """Result of a Claude Code agent execution."""

    task_id: str
    repo_path: str
    output: str = ""
    stderr: str = ""
    returncode: int = -1
    pr_url: str | None = None
    duration_seconds: float = 0.0
    completed_at: datetime = Field(default_factory=lambda: datetime.now(UTC))

    @property
    def succeeded(self) -> bool:
        return self.returncode == 0


class AgentRun(BaseModel):
    """Tracking record for an active or completed agent run."""

    task: WorkItem
    started_at: datetime = Field(default_factory=lambda: datetime.now(UTC))
    result: AgentResult | None = None

    @property
    def is_active(self) -> bool:
        return self.result is None


class OrchestratorState(BaseModel):
    """Top-level durable state for the orchestrator."""

    active_runs: list[AgentRun] = Field(default_factory=list)
    completed_runs: list[AgentRun] = Field(default_factory=list)
    merge_queue: list[PRInfo] = Field(default_factory=list)
    work_queue: list[WorkItem] = Field(default_factory=list)
    last_scan: datetime | None = None
    last_discovery: datetime | None = None
    cycle_count: int = 0


class AutoloopConfig(BaseSettings):
    """Configuration for radioactive-ralph.

    Every field can be set via:

    1. TOML config file (``~/.radioactive-ralph/config.toml``)
    2. Environment variable with prefix ``RALPH_`` (e.g. ``RALPH_MAX_PARALLEL_AGENTS=10``)
    3. The defaults below

    Example config.toml::

        [orgs]
        arcade-cabinet = "~/src/arcade-cabinet"
        jbcom = "~/src/jbcom"

        bulk_model = "claude-haiku-4-5-20251001"
        max_parallel_agents = 8
    """

    model_config = SettingsConfigDict(
        env_prefix="RALPH_",
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )

    # ── Repo discovery ──────────────────────────────────────────────────
    orgs: dict[str, str] = Field(
        default_factory=dict,
        description="Org name → local directory path mapping",
    )

    # ── Model selection ─────────────────────────────────────────────────
    bulk_model: str = Field(
        default="claude-haiku-4-5-20251001",
        description="Model for bulk/mechanical work (doc sweeps, missing files)",
    )
    default_model: str = Field(
        default="claude-sonnet-4-6",
        description="Default model for feature work and bug fixes",
    )
    deep_model: str = Field(
        default="claude-opus-4-6",
        description="Model for architecture and design decisions",
    )

    # ── Concurrency ──────────────────────────────────────────────────────
    max_parallel_agents: int = Field(
        default=5,
        description="Maximum concurrent Claude Code subprocesses",
    )
    max_parallel_doc_sweep: int = Field(
        default=10,
        description="Maximum concurrent agents for doc-sweep batches",
    )

    # ── Timeouts ─────────────────────────────────────────────────────────
    agent_timeout_minutes: int = Field(
        default=30,
        description="Kill an agent after this many minutes of silence",
    )
    orphan_threshold_hours: float = Field(
        default=2.0,
        description="Re-queue active_runs older than this many hours on startup",
    )

    # ── Cycle backoff ─────────────────────────────────────────────────────
    cycle_backoff_base_s: float = Field(
        default=30.0,
        description="Base inter-cycle sleep in seconds",
    )
    cycle_backoff_max_s: float = Field(
        default=600.0,
        description="Maximum inter-cycle sleep in seconds (exponential backoff cap)",
    )
    cycle_backoff_factor: float = Field(
        default=2.0,
        description="Exponential backoff multiplier on consecutive errors",
    )

    # ── Forge rate limits ─────────────────────────────────────────────────
    rate_limit_default_wait_s: int = Field(
        default=60,
        description="Default 429 Retry-After wait when header is absent",
    )

    # ── State ─────────────────────────────────────────────────────────────
    state_path: str = Field(
        default="",
        description="Override for state file path (default: ~/.radioactive-ralph/state.json)",
    )

    def resolve_state_path(self) -> Path:
        """Resolve the state file path, expanding ~ and applying defaults."""
        if self.state_path:
            return Path(self.state_path).expanduser()
        return Path.home() / ".radioactive-ralph" / "state.json"

    def all_repo_paths(self) -> list[Path]:
        """Return all git repo paths discovered under configured org directories."""
        paths: list[Path] = []
        for org_path in self.orgs.values():
            expanded = Path(org_path).expanduser()
            if expanded.is_dir():
                for child in sorted(expanded.iterdir()):
                    if child.is_dir() and (child / ".git").exists():
                        paths.append(child)
        return paths
