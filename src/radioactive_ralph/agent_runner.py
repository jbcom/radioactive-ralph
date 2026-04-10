"""Spawn claude CLI subprocesses as agents."""

from __future__ import annotations

import asyncio
import os
import re
import time

from .models import AgentResult, WorkItem, WorkPriority


async def run_agent(
    task: WorkItem,
    model: str = "claude-sonnet-4-6",
    timeout: int = 1800,  # noqa: ASYNC109
) -> AgentResult:
    """Run a single claude CLI agent subprocess for a WorkItem."""
    start = time.monotonic()
    # asyncio.create_subprocess_exec never invokes a shell — no injection risk
    proc = await asyncio.create_subprocess_exec(
        "claude",
        "--model", model,
        "--print",
        "--dangerously-skip-permissions",
        task.description,
        cwd=task.repo_path,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
        env={**os.environ},
    )
    try:
        stdout, _ = await asyncio.wait_for(proc.communicate(), timeout=timeout)
        output = stdout.decode()
        return AgentResult(
            task_id=task.id,
            repo_path=task.repo_path,
            output=output,
            returncode=proc.returncode or 0,
            pr_url=_extract_pr_url(output),
            duration_seconds=time.monotonic() - start,
        )
    except TimeoutError:
        proc.kill()
        return AgentResult(
            task_id=task.id,
            repo_path=task.repo_path,
            returncode=124,
            duration_seconds=time.monotonic() - start,
        )


async def run_parallel_agents(
    tasks: list[WorkItem],
    model: str = "claude-sonnet-4-6",
    timeout: int = 1800,  # noqa: ASYNC109
) -> list[AgentResult]:
    """Run multiple agents in parallel, one per WorkItem."""
    return list(await asyncio.gather(*[run_agent(t, model, timeout) for t in tasks]))


def select_model(
    task: WorkItem,
    bulk: str = "claude-haiku-4-5-20251001",
    default: str = "claude-sonnet-4-6",
    deep: str = "claude-opus-4-6",
) -> str:
    """Pick the right model tier for a work item based on priority."""
    if task.priority in (WorkPriority.DOC_SWEEP, WorkPriority.MISSING_FILES):
        return bulk
    if task.priority == WorkPriority.DESIGN_FEATURE:
        return deep
    return default


def _extract_pr_url(output: str) -> str | None:
    match = re.search(r"https://github\.com/[^/\s]+/[^/\s]+/pull/\d+", output)
    return match.group(0) if match else None
