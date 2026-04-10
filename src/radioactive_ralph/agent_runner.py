"""Spawn claude CLI subprocesses as agents."""

from __future__ import annotations

import asyncio
import os
import re
import time

from .config import RadioactiveRalphConfig
from .models import AgentResult, WorkItem, WorkPriority


def build_agent_prompt(task: WorkItem, config: RadioactiveRalphConfig | None = None) -> str:
    """Wrap a work item's description with attribution instructions.

    If attribution is enabled on the config, the agent is instructed to append
    the configured trailer to its commit message and the attribution block to
    the PR body. This gives Ralph a small, visible credit on everything it
    produces — on by default, easy to disable.
    """
    if config is None or not config.attribution_enabled:
        return task.description

    trailer = config.commit_trailer()
    pr_attribution = config.pr_body_attribution().strip()

    return (
        f"{task.description}\n\n"
        "---\n"
        "## radioactive-ralph attribution (required)\n\n"
        "When you create a commit for this task, append this Git trailer on "
        "its own line, at the very end of the commit message body:\n\n"
        f"    {trailer}\n\n"
        "When you open a pull request for this task, append this block to the "
        "end of the PR body (after any existing content):\n\n"
        f"{pr_attribution}\n\n"
        "This attribution is required — it is how the operator tracks which "
        "work was done autonomously by radioactive-ralph."
    )


async def run_agent(
    task: WorkItem,
    model: str = "claude-sonnet-4-6",
    timeout: int = 1800,  # noqa: ASYNC109
    config: RadioactiveRalphConfig | None = None,
) -> AgentResult:
    """Run a single claude CLI agent subprocess for a WorkItem."""
    start = time.monotonic()
    prompt = build_agent_prompt(task, config)
    # asyncio.create_subprocess_exec never invokes a shell — no injection risk
    proc = await asyncio.create_subprocess_exec(
        "claude",
        "--model", model,
        "--print",
        "--dangerously-skip-permissions",
        prompt,
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
    config: RadioactiveRalphConfig | None = None,
) -> list[AgentResult]:
    """Run multiple agents in parallel, one per WorkItem."""
    return list(
        await asyncio.gather(*[run_agent(t, model, timeout, config) for t in tasks])
    )


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
