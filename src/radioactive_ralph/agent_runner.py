"""Spawn claude CLI subprocesses as agents.

Each :func:`run_agent` call launches one ``claude --print`` subprocess for a
:class:`~radioactive_ralph.models.WorkItem` and returns a structured
:class:`~radioactive_ralph.models.AgentResult`.

Model selection is delegated to :func:`select_model` so that bulk work
(doc sweeps) uses haiku, architecture decisions use opus, and everything
else uses sonnet. Use :func:`run_parallel_agents` to fan out a batch.

Example::

    results = await run_parallel_agents(work_queue, max_concurrent=5)
    for r in results:
        if not r.succeeded:
            logger.warning("Agent failed: %s\\n%s", r.task_id, r.stderr)
"""

from __future__ import annotations

import asyncio
import logging
import os
import re
import time

from .models import AgentResult, WorkItem, WorkPriority

logger = logging.getLogger(__name__)


async def run_agent(
    task: WorkItem,
    model: str = "claude-sonnet-4-6",
    timeout: int = 1800,  # noqa: ASYNC109
) -> AgentResult:
    """Run a single Claude Code agent subprocess for a WorkItem.

    Launches ``claude --model <model> --print --dangerously-skip-permissions``
    with the task description as input. Captures both stdout and stderr so
    failures are diagnosable.

    On timeout, the subprocess is killed and reaped (no zombie leak).

    Args:
        task: The work item to execute.
        model: Anthropic model ID to use.
        timeout: Maximum seconds to wait before killing the subprocess.

    Returns:
        An :class:`~radioactive_ralph.models.AgentResult` with output,
        returncode, stderr, and duration.
    """
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
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=timeout)
        output = stdout.decode()
        err_text = stderr.decode()
        rc = proc.returncode or 0
        if rc != 0:
            logger.warning(
                "Agent %s exited %d:\n%s", task.id, rc,
                err_text[:2000] if err_text else "(no stderr)",
            )
        return AgentResult(
            task_id=task.id,
            repo_path=task.repo_path,
            output=output,
            stderr=err_text,
            returncode=rc,
            pr_url=_extract_pr_url(output),
            duration_seconds=time.monotonic() - start,
        )
    except TimeoutError:
        proc.kill()
        await proc.wait()  # reap the zombie — critical for long-running daemons
        logger.warning("Agent %s timed out after %ds", task.id, timeout)
        return AgentResult(
            task_id=task.id,
            repo_path=task.repo_path,
            returncode=124,
            duration_seconds=time.monotonic() - start,
        )


async def run_parallel_agents(
    tasks: list[WorkItem],
    max_concurrent: int = 5,
    bulk_model: str = "claude-haiku-4-5-20251001",
    default_model: str = "claude-sonnet-4-6",
    deep_model: str = "claude-opus-4-6",
    timeout: int = 1800,  # noqa: ASYNC109
) -> list[AgentResult]:
    """Run multiple agents concurrently, capped at *max_concurrent*.

    Each task is assigned a model via :func:`select_model` before dispatch,
    ensuring DOC_SWEEP tasks use haiku and DESIGN_FEATURE tasks use opus.

    Args:
        tasks: Work items to execute.
        max_concurrent: Maximum number of parallel subprocesses.
        bulk_model: Model for low-priority bulk tasks (haiku).
        default_model: Default model for most tasks (sonnet).
        deep_model: Model for architecture/design tasks (opus).
        timeout: Per-agent timeout in seconds.

    Returns:
        Results in the same order as *tasks*.
    """
    semaphore = asyncio.Semaphore(max_concurrent)

    async def _run_one(task: WorkItem) -> AgentResult:
        model = select_model(task, bulk=bulk_model, default=default_model, deep=deep_model)
        async with semaphore:
            return await run_agent(task, model=model, timeout=timeout)

    return list(await asyncio.gather(*(_run_one(t) for t in tasks)))


def select_model(
    task: WorkItem,
    bulk: str = "claude-haiku-4-5-20251001",
    default: str = "claude-sonnet-4-6",
    deep: str = "claude-opus-4-6",
) -> str:
    """Select the appropriate model tier for a work item.

    Model tiers (lowest to highest cost):

    - **haiku**: DOC_SWEEP, MISSING_FILES — mechanical, repetitive work
    - **opus**: DESIGN_FEATURE — requires deep reasoning and vision
    - **sonnet**: everything else — the balanced default

    Args:
        task: Work item whose priority drives model selection.
        bulk: Model ID for bulk/mechanical work.
        default: Model ID for standard feature work.
        deep: Model ID for architecture/design decisions.

    Returns:
        Model ID string ready for ``--model`` flag.
    """
    if task.priority in (WorkPriority.DOC_SWEEP, WorkPriority.MISSING_FILES):
        return bulk
    if task.priority == WorkPriority.DESIGN_FEATURE:
        return deep
    return default


def _extract_pr_url(output: str) -> str | None:
    """Extract a PR/MR URL from agent text output.

    Matches GitHub PRs and GitLab merge requests.

    Args:
        output: Raw stdout from the agent subprocess.

    Returns:
        First matching URL, or None.
    """
    match = re.search(
        r"https://[^\s/]+/[^\s/]+/[^\s/]+/(?:pull|merge_requests)/\d+",
        output,
    )
    return match.group(0) if match else None
