"""Tests for Pydantic models."""

from __future__ import annotations

from radioactive_ralph.config import RadioactiveRalphConfig
from radioactive_ralph.models import (
    AgentRun,
    OrchestratorState,
    PRInfo,
    PRStatus,
    ReviewFinding,
    ReviewResult,
    ReviewSeverity,
    WorkItem,
    WorkPriority,
)


def test_pr_status_values() -> None:
    """Test that PRStatus has expected values."""
    assert PRStatus.MERGE_READY.value == "merge_ready"
    assert PRStatus.NEEDS_REVIEW.value == "needs_review"


def test_pr_info_mergeable() -> None:
    """Test the is_mergeable property logic."""
    from datetime import UTC, datetime
    
    pr = PRInfo(
        repo="org/repo",
        number=1,
        title="test",
        author="user",
        branch="main",
        url="http://url",
        status=PRStatus.MERGE_READY,
        updated_at=datetime.now(UTC),
        ci_passed=True,
        is_draft=False
    )
    assert pr.is_mergeable is True

    pr.ci_passed = False
    assert pr.is_mergeable is False


def test_radioactive_ralph_config_defaults(mocker) -> None:
    """Test that RadioactiveRalphConfig has correct default values.
    
    Args:
        mocker: The pytest-mock fixture.
    """
    # Isolate from host environment variables and config files
    mocker.patch.dict("os.environ", {}, clear=True)
    mocker.patch("radioactive_ralph.config._resolve_toml_path", return_value=__import__("pathlib").Path("/nonexistent"))
    
    cfg = RadioactiveRalphConfig()
    assert "claude-sonnet" in cfg.default_model
    assert cfg.max_parallel_agents == 5


def test_work_item_repo_name() -> None:
    """Test that WorkItem correctly extracts repo name from path."""
    item = WorkItem(
        id="123",
        repo_path="/srv/projects/my-app",
        description="test",
        priority=WorkPriority.LOW
    )
    assert item.repo_name == "my-app"


def test_orchestrator_state_defaults() -> None:
    """Test default values for OrchestratorState."""
    state = OrchestratorState()
    assert state.active_runs == []
    assert state.cycle_count == 0
    assert state.work_queue == []


def test_agent_run_is_active() -> None:
    """Test the is_active property logic."""
    from datetime import UTC, datetime
    item = WorkItem(id="1", repo_path="p", description="d", priority=WorkPriority.LOW)
    run = AgentRun(item=item, process_id=123, started_at=datetime.now(UTC))
    assert run.is_active is True
    
    run.completed_at = datetime.now(UTC)
    assert run.is_active is False


def test_review_result_has_blocking_issues():
    pr = PRInfo(
        repo="r", number=1, title="t", author="a", branch="b",
        url="u", status=PRStatus.NEEDS_REVIEW, 
        updated_at=__import__("datetime").datetime.now(__import__("datetime").UTC)
    )
    res = ReviewResult(pr=pr, approved=False, summary="s", findings=[])
    assert res.has_blocking_issues is False
    
    res.findings = [ReviewFinding(severity=ReviewSeverity.ERROR, file="f", issue="i")]
    assert res.has_blocking_issues is True
