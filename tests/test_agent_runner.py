import pytest
import asyncio
from radioactive_ralph.agent_runner import run_parallel_agents, _spawn_agent
from radioactive_ralph.models import WorkItem, WorkPriority, AgentRun
from radioactive_ralph.config import RadioactiveRalphConfig

@pytest.fixture
def mock_config():
    return RadioactiveRalphConfig()

@pytest.mark.asyncio
async def test_run_parallel_agents_empty(mock_config):
    runs = await run_parallel_agents([], mock_config, 5)
    assert runs == []

@pytest.mark.asyncio
async def test_run_parallel_agents_zero_spawn(mock_config):
    queue = [WorkItem(id="test_0", repo_name="test", repo_path=".", description="test", priority=WorkPriority.HIGH)]
    runs = await run_parallel_agents(queue, mock_config, 0)
    assert runs == []
    assert len(queue) == 1

@pytest.mark.asyncio
async def test_run_parallel_agents_success(mock_config, mocker):
    item1 = WorkItem(id="test_1", repo_name="test", repo_path=".", description="test1", priority=WorkPriority.HIGH)
    item2 = WorkItem(id="test_2", repo_name="test", repo_path=".", description="test2", priority=WorkPriority.LOW)
    queue = [item2, item1] 
    
    mock_proc = mocker.Mock()
    mock_proc.pid = 1234
    
    mock_create = mocker.patch("asyncio.create_subprocess_exec", return_value=mock_proc)
    
    runs = await run_parallel_agents(queue, mock_config, 5)
    
    assert len(runs) == 2
    assert len(queue) == 0
    assert runs[0].item.description == "test1" 
    assert runs[0].process_id == 1234
    assert mock_create.call_count == 2

@pytest.mark.asyncio
async def test_run_parallel_agents_exception(mock_config, mocker):
    item = WorkItem(id="test_1", repo_name="test", repo_path=".", description="test", priority=WorkPriority.HIGH)
    queue = [item]
    
    mocker.patch("asyncio.create_subprocess_exec", side_effect=Exception("Failed to spawn"))
    
    runs = await run_parallel_agents(queue, mock_config, 5)
    
    assert len(runs) == 1
    assert runs[0].process_id == -1
