"""Main orchestration loop — the daemon that drives everything.

The :class:`Orchestrator` runs indefinitely, executing cycles of:

1. **PR scan** — classify all open PRs across all repos
2. **Merge** — squash-merge any MERGE_READY PRs
3. **Review** — AI-review PRs that need review
4. **Discover** — find new work items from STATE.md and missing files
5. **Execute** — spawn Claude Code agents for the highest-priority work

Cycle timing uses adaptive backoff: 30 s normally, up to 10 min on repeated
failures. State is persisted to JSON after every cycle so restarts are safe.

On startup, orphaned ``active_runs`` (tasks that were running when the
daemon crashed) are detected by age and re-queued rather than left as ghosts.
"""

from __future__ import annotations

import asyncio
import contextlib
import logging
import signal
from datetime import UTC, datetime, timedelta
from pathlib import Path

from .agent_runner import run_parallel_agents
from .config import load_config
from .models import (
    AgentResult,
    AgentRun,
    AutoloopConfig,
    PRInfo,
    PRStatus,
    WorkItem,
)
from .pr_manager import merge_pr, scan_all_repos
from .reviewer import review_pr
from .state import load_state, merge_work_items, prune_completed, save_state
from .work_discovery import discover_all_repos

logger = logging.getLogger(__name__)

# Backoff defaults — overridden by config fields at runtime
_BACKOFF_BASE = 30.0
_BACKOFF_MAX = 600.0
_BACKOFF_FACTOR = 2.0


class Orchestrator:
    """Autonomous development orchestrator — the main daemon.

    Args:
        config: Parsed configuration. Loaded from default path if omitted.
        state_path: Path to the JSON state file. Resolved from config if omitted.

    Example::

        cfg = load_config(Path("~/.radioactive-ralph/config.toml").expanduser())
        await Orchestrator(cfg).run()
    """

    def __init__(
        self,
        config: AutoloopConfig | None = None,
        state_path: Path | None = None,
    ) -> None:
        self.config = config or load_config()
        self.state_path = state_path or self.config.resolve_state_path()
        self.state = load_state(self.state_path)
        self._shutdown = asyncio.Event()
        self._consecutive_errors = 0

    def _register_signals(self) -> None:
        """Register SIGINT/SIGTERM handlers (must be called inside a running loop)."""
        loop = asyncio.get_running_loop()
        for sig in (signal.SIGINT, signal.SIGTERM):
            loop.add_signal_handler(sig, self._request_shutdown)

    def _request_shutdown(self) -> None:
        logger.info("Shutdown requested — finishing active agents...")
        self._shutdown.set()

    def _persist(self) -> None:
        save_state(self.state, self.state_path)

    def _recover_orphans(self) -> None:
        """Move stale active_runs back to work_queue on startup.

        If the daemon crashed while agents were running, those AgentRun entries
        remain in active_runs with no result. Detect them by age and re-enqueue
        the tasks so they are not silently lost.
        """
        orphan_threshold = timedelta(hours=self.config.orphan_threshold_hours)
        now = datetime.now(UTC)
        orphans: list[AgentRun] = []
        survivors: list[AgentRun] = []

        for run in self.state.active_runs:
            age = now - run.started_at
            if age > orphan_threshold:
                orphans.append(run)
            else:
                survivors.append(run)

        if orphans:
            logger.warning(
                "Recovering %d orphaned active_runs (age > %gh) → re-queuing tasks",
                len(orphans),
                self.config.orphan_threshold_hours,
            )
            recovered = [o.task for o in orphans]
            merge_work_items(self.state, recovered)

        self.state.active_runs = survivors

    async def run(self) -> None:
        """Main loop — scan, merge, review, discover, execute, repeat.

        Adaptive backoff on consecutive errors prevents hammering a broken
        environment. Persists state after every cycle for crash safety.
        """
        self._register_signals()  # must be inside a running event loop
        self._recover_orphans()
        self._persist()

        logger.info("Orchestrator starting with %d configured orgs", len(self.config.orgs))

        while not self._shutdown.is_set():
            self.state.cycle_count += 1
            logger.info("=== Cycle %d ===", self.state.cycle_count)

            try:
                await self._cycle()
                self._consecutive_errors = 0
                sleep_s = _BACKOFF_BASE
            except Exception:
                logger.exception("Error in orchestration cycle")
                self._consecutive_errors += 1
                sleep_s = min(
                    _BACKOFF_BASE * (_BACKOFF_FACTOR ** self._consecutive_errors),
                    _BACKOFF_MAX,
                )
                logger.info(
                    "Backing off %ds after %d consecutive error(s)",
                    int(sleep_s),
                    self._consecutive_errors,
                )

            self._persist()

            with contextlib.suppress(TimeoutError):
                await asyncio.wait_for(self._shutdown.wait(), timeout=sleep_s)

        logger.info("Orchestrator shutting down after %d cycles", self.state.cycle_count)
        self._persist()

    async def _cycle(self) -> None:
        """Execute one full orchestration cycle."""
        repo_paths = self.config.all_repo_paths()
        if not repo_paths:
            logger.warning("No repos configured — nothing to do")
            return

        all_prs = await self._scan_prs(repo_paths)
        await self._drain_merge_queue(all_prs)
        await self._review_pending(all_prs)
        await self._discover_and_execute(repo_paths)

        prune_completed(self.state, keep=100)

    async def _scan_prs(self, repo_paths: list[Path]) -> dict[str, list[PRInfo]]:
        """Scan all repos for PRs and update last_scan timestamp."""
        logger.info("Scanning %d repos for PRs...", len(repo_paths))
        all_prs = await scan_all_repos(repo_paths)
        self.state.last_scan = datetime.now(UTC)

        total = sum(len(prs) for prs in all_prs.values())
        logger.info("Found %d open PRs across %d repos", total, len(all_prs))
        return all_prs

    async def _drain_merge_queue(self, all_prs: dict[str, list[PRInfo]]) -> None:
        """Merge all PRs that are MERGE_READY."""
        merge_ready = [
            (repo_path, pr)
            for repo_path, prs in all_prs.items()
            for pr in prs
            if pr.is_mergeable
        ]

        if not merge_ready:
            return

        logger.info("Merging %d ready PRs", len(merge_ready))
        for repo_path, pr in merge_ready:
            success = await merge_pr(pr, Path(repo_path))
            if success:
                logger.info("Merged PR #%d in %s", pr.number, pr.repo)
            else:
                logger.warning("Failed to merge PR #%d in %s", pr.number, pr.repo)

    async def _review_pending(self, all_prs: dict[str, list[PRInfo]]) -> None:
        """AI-review PRs that need review."""
        needs_review = [
            (repo_path, pr)
            for repo_path, prs in all_prs.items()
            for pr in prs
            if pr.status == PRStatus.NEEDS_REVIEW
        ]

        if not needs_review:
            return

        logger.info("Reviewing %d PRs", len(needs_review))
        for repo_path, pr in needs_review:
            result = await review_pr(pr, repo_path, model=self.config.bulk_model)
            if result.approved:
                logger.info("PR #%d in %s: approved", pr.number, pr.repo)
            else:
                logger.info(
                    "PR #%d in %s: %d findings",
                    pr.number,
                    pr.repo,
                    len(result.findings),
                )

    async def _discover_and_execute(self, repo_paths: list[Path]) -> None:
        """Discover new work items and spawn agents for the highest-priority batch."""
        items = discover_all_repos(repo_paths)
        added = merge_work_items(self.state, items)
        self.state.last_discovery = datetime.now(UTC)
        logger.info(
            "Discovered %d new work items (queue: %d)",
            added, len(self.state.work_queue),
        )

        available = self.config.max_parallel_agents - len(self.state.active_runs)
        if available <= 0 or not self.state.work_queue:
            return

        batch: list[WorkItem] = self.state.work_queue[:available]
        self.state.work_queue = self.state.work_queue[available:]

        for task in batch:
            self.state.active_runs.append(AgentRun(task=task))

        self._persist()  # persist before spawning so tasks are tracked on crash

        results: list[AgentResult] = await run_parallel_agents(
            batch,
            max_concurrent=self.config.max_parallel_agents,
            bulk_model=self.config.bulk_model,
            default_model=self.config.default_model,
            deep_model=self.config.deep_model,
        )

        for result in results:
            for run in self.state.active_runs:
                if run.task.id == result.task_id:
                    run.result = result
                    self.state.completed_runs.append(run)
                    break

            if result.pr_url:
                logger.info("Agent created PR: %s", result.pr_url)

        self.state.active_runs = [r for r in self.state.active_runs if r.is_active]


async def start_orchestrator(
    config: AutoloopConfig | None = None,
    state_path: Path | None = None,
) -> None:
    """Entry point for the orchestrator daemon.

    Args:
        config: Parsed configuration. Loaded from default path if omitted.
        state_path: Path to the state file. Resolved from config if omitted.
    """
    orch = Orchestrator(config=config, state_path=state_path)
    await orch.run()
