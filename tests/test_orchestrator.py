import pytest
import asyncio
from pathlib import Path
from radioactive_ralph.orchestrator import Orchestrator
from radioactive_ralph.models import PRInfo, PRStatus, WorkItem, WorkPriority, AgentRun
from radioactive_ralph.config import RadioactiveRalphConfig
from radioactive_ralph.ralph_says import Variant
from datetime import datetime, UTC

@pytest.fixture
def mock_config(mocker):
    config = RadioactiveRalphConfig()
    mocker.patch("radioactive_ralph.config.RadioactiveRalphConfig.all_repo_paths", return_value=[Path("/path/to/repo")])
    config.cycle_sleep_seconds = 0.01
    return config

@pytest.fixture
def mock_state(mocker):
    mock_load = mocker.patch("radioactive_ralph.orchestrator.load_state")
    mocker.patch("radioactive_ralph.orchestrator.save_state")
    state = mocker.Mock()
    state.work_queue = []
    state.active_runs = []
    state.last_discovery = None
    state.cycle_count = 0
    mock_load.return_value = state
    return state

@pytest.mark.asyncio
async def test_orchestrator_stop(mock_config, mock_state):
    orchestrator = Orchestrator(config=mock_config)
    orchestrator.stop()
    assert orchestrator._stop_event.is_set()

@pytest.mark.asyncio
async def test_orchestrator_run_and_step(mock_config, mock_state, mocker):
    mocker.patch("radioactive_ralph.orchestrator.scan_all_repos", return_value={Path("/path/to/repo"): []})
    mocker.patch("radioactive_ralph.orchestrator.discover_all_repos", return_value=[])
    
    orchestrator = Orchestrator(config=mock_config)
    
    task = asyncio.create_task(orchestrator.run())
    await asyncio.sleep(0.05)
    orchestrator.stop()
    await task
    
    assert mock_state.cycle_count > 0

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
        ci_passed=True
    )
    pr_not_ready = PRInfo(
        number=2, 
        title="Test", 
        status=PRStatus.UNKNOWN, 
        url="http", 
        repo="test",
        author="ralph",
        branch="main",
        updated_at=datetime.now(UTC)
    )
    
    mock_merge_pr = mocker.patch("radioactive_ralph.orchestrator.merge_pr", side_effect=[True, False])
    
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
        updated_at=datetime.now(UTC)
    )
    pr_open = PRInfo(
        number=2, 
        title="Test", 
        status=PRStatus.UNKNOWN, 
        url="http", 
        repo="test",
        author="ralph",
        branch="main",
        updated_at=datetime.now(UTC)
    )
    
    mock_forge_client_cm = mocker.AsyncMock()
    mock_forge_client = mocker.Mock()
    mock_forge_client_cm.__aenter__.return_value = mock_forge_client
    mocker.patch("radioactive_ralph.orchestrator.get_forge_client", return_value=mock_forge_client_cm)
    
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

@pytest.mark.asyncio
async def test_step_spawns_agents(mock_config, mock_state, mocker):
    mocker.patch("radioactive_ralph.orchestrator.scan_all_repos", return_value={})
    mocker.patch("radioactive_ralph.orchestrator.discover_all_repos", return_value=[])
    mock_state.work_queue = [WorkItem(id="1", repo_name="a", repo_path="b", description="c", priority=WorkPriority.HIGH)]
    mock_config.max_parallel_agents = 2
    
    async def mock_run_agents(*args, **kwargs):
        return [AgentRun(item=mock_state.work_queue[0], process_id=123, started_at=datetime.now(UTC))]
    
    mocker.patch("radioactive_ralph.orchestrator.run_parallel_agents", side_effect=mock_run_agents)
    mocker.patch("radioactive_ralph.orchestrator.prune_completed", return_value=[])
    
    orchestrator = Orchestrator(config=mock_config)
    await orchestrator._step()
    
    assert len(mock_state.active_runs) == 1
