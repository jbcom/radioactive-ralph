"""Tests for the orchestrator.

Under rewrite — `run`, `stop`, and `_step` were removed in M1 and replaced with
NotImplementedError stubs that point at docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md.
The remaining tests cover preserved helpers (`_merge_ready`, `_review_pending`,
`_should_discover`) because they're still useful building blocks the M2 daemon
will reuse.
"""

from datetime import UTC, datetime
from pathlib import Path

import pytest

from radioactive_ralph.config import RadioactiveRalphConfig
from radioactive_ralph.models import PRInfo, PRStatus
from radioactive_ralph.orchestrator import Orchestrator


@pytest.fixture
def mock_config(mocker):
    config = RadioactiveRalphConfig()
    mocker.patch(
        "radioactive_ralph.config.RadioactiveRalphConfig.all_repo_paths",
        return_value=[Path("/path/to/repo")],
    )
    config.cycle_sleep_seconds = 0.01
    return config


@pytest.fixture
def mock_state(mocker):
    mock_load = mocker.patch("radioactive_ralph.orchestrator.load_state")
    state = mocker.Mock()
    state.work_queue = []
    state.active_runs = []
    state.last_discovery = None
    state.cycle_count = 0
    mock_load.return_value = state
    return state


@pytest.mark.asyncio
async def test_orchestrator_run_raises_not_implemented(mock_config, mock_state):
    """`run` is stubbed pending the M2 daemon rewrite."""
    orchestrator = Orchestrator(config=mock_config)
    with pytest.raises(NotImplementedError, match="under rewrite"):
        await orchestrator.run()


def test_orchestrator_stop_raises_not_implemented(mock_config, mock_state):
    """`stop` is stubbed pending the M2 daemon rewrite."""
    orchestrator = Orchestrator(config=mock_config)
    with pytest.raises(NotImplementedError, match="under rewrite"):
        orchestrator.stop()


@pytest.mark.asyncio
async def test_merge_ready(mock_config, mock_state, mocker):
    pr_ready = PRInfo(
        number=1,
        title="Test",
        status=PRStatus.MERGE_READY,
        url="http",
        repo="test",
        author="ralph",
        branch="main",
        updated_at=datetime.now(UTC),
        ci_passed=True,
    )
    pr_not_ready = PRInfo(
        number=2,
        title="Test",
        status=PRStatus.UNKNOWN,
        url="http",
        repo="test",
        author="ralph",
        branch="main",
        updated_at=datetime.now(UTC),
    )

    mock_merge_pr = mocker.patch(
        "radioactive_ralph.orchestrator.merge_pr", side_effect=[True, False]
    )

    orchestrator = Orchestrator(config=mock_config)

    await orchestrator._merge_ready({"/path/to/repo": [pr_ready, pr_not_ready]})
    mock_merge_pr.assert_called_once()

    mock_merge_pr.reset_mock()
    await orchestrator._merge_ready({"/path/to/repo": [pr_ready]})
    mock_merge_pr.assert_called_once()


@pytest.mark.asyncio
async def test_review_pending(mock_config, mock_state, mocker):
    pr_needs_review = PRInfo(
        number=1,
        title="Test",
        status=PRStatus.NEEDS_REVIEW,
        url="http",
        repo="test",
        author="ralph",
        branch="main",
        updated_at=datetime.now(UTC),
    )
    pr_open = PRInfo(
        number=2,
        title="Test",
        status=PRStatus.UNKNOWN,
        url="http",
        repo="test",
        author="ralph",
        branch="main",
        updated_at=datetime.now(UTC),
    )

    mock_forge_client_cm = mocker.AsyncMock()
    mock_forge_client = mocker.Mock()
    mock_forge_client_cm.__aenter__.return_value = mock_forge_client
    mocker.patch(
        "radioactive_ralph.orchestrator.get_forge_client",
        return_value=mock_forge_client_cm,
    )

    mock_review_pr = mocker.patch("radioactive_ralph.orchestrator.review_pr")
    mock_review_pr.return_value.approved = True

    orchestrator = Orchestrator(config=mock_config)
    await orchestrator._review_pending({"/path/to/repo": [pr_needs_review, pr_open]})

    mock_review_pr.assert_called_once()

    mock_review_pr.reset_mock()
    mock_review_pr.return_value.approved = False
    mock_review_pr.return_value.findings = ["issue 1"]
    await orchestrator._review_pending({"/path/to/repo": [pr_needs_review]})
    mock_review_pr.assert_called_once()


def test_should_discover(mock_config, mock_state):
    orchestrator = Orchestrator(config=mock_config)

    assert orchestrator._should_discover() is True

    mock_state.last_discovery = datetime.now(UTC)
    assert orchestrator._should_discover() is False

    mock_state.last_discovery = datetime(2000, 1, 1, tzinfo=UTC)
    assert orchestrator._should_discover() is True
