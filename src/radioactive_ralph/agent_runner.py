"""Parallel execution of Claude Code agents."""

from __future__ import annotations

import asyncio
import logging
import subprocess
from datetime import UTC, datetime
from typing import TYPE_CHECKING, cast

from radioactive_ralph.models import AgentRun, WorkItem

if TYPE_CHECKING:
    from radioactive_ralph.config import RadioactiveRalphConfig

logger = logging.getLogger(__name__)


async def run_parallel_agents(
    queue: list[WorkItem],
    config: RadioactiveRalphConfig,
    max_spawn: int,
) -> list[AgentRun]:
    """Spawn new agent runs for queued work items up to max_spawn.

    Args:
        queue: The current work queue.
        config: Orchestrator configuration.
        max_spawn: Maximum number of new agents to start.

    Returns:
        List of newly started AgentRun objects.
    """
    if not queue or max_spawn <= 0:
        return []

    # Sort queue by priority (high first) and age
    queue.sort(key=lambda x: (x.priority.value, x.created_at), reverse=True)

    to_spawn = queue[:max_spawn]
    new_runs = []

    for item in to_spawn:
        run = await _spawn_agent(item, config)
        new_runs.append(run)
        queue.remove(item)

    return new_runs


async def _spawn_agent(item: WorkItem, config: RadioactiveRalphConfig) -> AgentRun:
    """Launch a single Claude Code agent subprocess for a work item.

    Args:
        item: The work item to fulfill.
        config: Orchestrator configuration.

    Returns:
        An AgentRun object tracking the background process.
    """
    cmd = ["claude", "--message", item.description, "--yes"]

    # In a real environment, we might use a wrapper to capture output or
    # run inside a specific container/VM.
    logger.info("Spawning agent for %s: %s", item.repo_name, item.description)

    try:
        # For this prototype, we use a non-blocking subprocess call
        # but don't actually wait for completion here.
        proc = await asyncio.create_subprocess_exec(
            *cmd,
            cwd=item.repo_path,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )

        run = AgentRun(
            item=item,
            process_id=proc.pid,
            started_at=datetime.now(UTC),
        )
        # Store the process object internally for monitoring (private field not in model)
        from typing import Any
        cast(Any, run)._proc = proc
        return run
    except Exception as e:
        logger.error("Failed to spawn agent: %s", e)
        # Return a "failed" run object or handle error
        return AgentRun(
            item=item,
            process_id=-1,
            started_at=datetime.now(UTC),
        )
