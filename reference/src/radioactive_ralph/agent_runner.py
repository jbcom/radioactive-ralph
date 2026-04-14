"""Parallel execution of Claude Code agents.

Under rewrite — see docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md (M2).
The previous implementation spawned `claude --message --yes`, which does not match
any real Claude CLI flag. The replacement uses `claude -p --input-format stream-json`
with piped stdin/stdout, per the PRD.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from radioactive_ralph.models import AgentRun, WorkItem

if TYPE_CHECKING:
    from radioactive_ralph.config import RadioactiveRalphConfig

_REWRITE_MSG = (
    "Agent spawning is under rewrite — see "
    "docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md (M2). "
    "The daemon will spawn `claude -p --input-format stream-json` subprocesses "
    "managed per worktree, not the legacy `claude --message --yes` invocation."
)


async def run_parallel_agents(
    queue: list[WorkItem],
    config: RadioactiveRalphConfig,
    max_spawn: int,
) -> list[AgentRun]:
    """Spawn new agent runs for queued work items up to max_spawn.

    Under rewrite — see module docstring and PRD.
    """
    raise NotImplementedError(_REWRITE_MSG)
