"""Tests for Pydantic models."""

from __future__ import annotations

from radioactive_ralph.models import (
    AutoloopConfig,
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
    assert PRStatus.MERGE_READY == "merge_ready"
    assert PRStatus.CI_FAILING == "ci_failing"


def test_work_priority_ordering() -> None:
    assert WorkPriority.CI_FAILURE < WorkPriority.PR_FIXES
    assert WorkPriority.PR_FIXES < WorkPriority.DOC_SWEEP
    assert WorkPriority.DOC_SWEEP < WorkPriority.POLISH


def test_pr_info_is_mergeable() -> None:
    pr = PRInfo(
        repo="test/repo",
        number=1,
        title="Test",
        author="bot",
        branch="feat/x",
        status=PRStatus.MERGE_READY,
        ci_passed=True,
        is_draft=False,
    )
    assert pr.is_mergeable is True


def test_pr_info_not_mergeable_if_draft() -> None:
    pr = PRInfo(
        repo="test/repo",
        number=2,
        title="WIP",
        author="bot",
        branch="feat/y",
        status=PRStatus.MERGE_READY,
        ci_passed=True,
        is_draft=True,
    )
    assert pr.is_mergeable is False


def test_review_result_blocking() -> None:
    pr = PRInfo(
        repo="r", number=1, title="t", author="a", branch="b", status=PRStatus.NEEDS_REVIEW
    )
    result = ReviewResult(
        pr=pr,
        findings=[
            ReviewFinding(severity=ReviewSeverity.ERROR, file="foo.py", issue="bad", fix="fix it"),
            ReviewFinding(severity=ReviewSeverity.NITPICK, file="bar.py", issue="meh", fix="ok"),
        ],
    )
    assert result.has_blocking_issues is True


def test_orchestrator_state_defaults() -> None:
    state = OrchestratorState()
    assert state.active_runs == []
    assert state.cycle_count == 0


def test_autoloop_config_defaults() -> None:
    cfg = AutoloopConfig()
    assert "claude-sonnet" in cfg.default_model
    assert cfg.max_parallel_agents == 5


def test_work_item_repo_name() -> None:
    item = WorkItem(
        id="x",
        repo_path="/home/user/src/my-repo",
        description="do stuff",
        priority=WorkPriority.DOC_SWEEP,
    )
    assert item.repo_name == "my-repo"
