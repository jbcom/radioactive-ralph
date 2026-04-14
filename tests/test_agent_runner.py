"""Tests for agent_runner.

Under rewrite — `run_parallel_agents` is stubbed during M1 because the previous
implementation called `claude --message --yes`, which is not a real flag. The
replacement lands in M2 (stream-json subprocess control).
"""

import pytest

from radioactive_ralph.agent_runner import run_parallel_agents
from radioactive_ralph.config import RadioactiveRalphConfig
from radioactive_ralph.models import WorkItem, WorkPriority


@pytest.fixture
def mock_config():
    return RadioactiveRalphConfig()


@pytest.mark.asyncio
async def test_run_parallel_agents_is_stubbed(mock_config):
    """Raises NotImplementedError with a pointer to the PRD."""
    queue = [
        WorkItem(
            id="test_0",
            repo_name="test",
            repo_path=".",
            description="test",
            priority=WorkPriority.HIGH,
        )
    ]
    with pytest.raises(NotImplementedError, match="under rewrite"):
        await run_parallel_agents(queue, mock_config, 5)
