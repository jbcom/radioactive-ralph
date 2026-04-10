"""Main orchestration loop — the daemon that drives everything."""

from __future__ import annotations

import asyncio
import logging
import signal
from datetime import UTC, datetime
from pathlib import Path

from .agent_runner import run_parallel_agents
from .config import load_config
from .models import (
    AgentRun,
    AutoloopConfig,
    PRInfo,
    PRStatus,
)
from .pr_manager import merge_pr, scan_all_repos, sync_after_merge
from .reviewer import review_pr
from .state import load_state, merge_work_items, prune_completed, save_state
from .work_discovery import discover_all_repos

logger = logging.getLogger(__name__)


class Orchestrator:
    """Autonomous development orchestrator — the main daemon."""

    def __init__(self, config: AutoloopConfig | None = None, state_path: Path | None = None):
        self.config = config or load_config()
        self.state_path = state_path or self.config.resolve_state_path()
        self.state = load_state(self.state_path)
        self._shutdown = asyncio.Event()
        self._setup_signals()

    def _setup_signals(self) -> None:
        """Register signal handlers for graceful shutdown."""
        try:
            loop = asyncio.get_running_loop()
            for sig in (signal.SIGINT, signal.SIGTERM):
                loop.add_signal_handler(sig, self._request_shutdown)
        except RuntimeError:
            pass

    def _request_shutdown(self) -> None:
        logger.info("Shutdown requested — finishing active agents...")
        self._shutdown.set()

    def _persist(self) -> None:
        save_state(self.state, self.state_path)

    async def run(self) -> None:
        """Main loop — scan, merge, review, discover, execute, repeat."""
        logger.info("Orchestrator starting with %d configured orgs", len(self.config.orgs))

        while not self._shutdown.is_set():
            self.state.cycle_count += 1
            logger.info("=== Cycle %d ===", self.state.cycle_count)

            try:
                await self._cycle()
            except Exception:
                logger.exception("Error in orchestration cycle")

            self._persist()

            import contextlib

            with contextlib.suppress(TimeoutError):
                await asyncio.wait_for(self._shutdown.wait(), timeout=30.0)

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
        """Scan all repos for PRs."""
        logger.info("Scanning %d repos for PRs...", len(repo_paths))
        all_prs = await scan_all_repos(repo_paths)
        self.state.last_scan = datetime.now(UTC)

        total = sum(len(prs) for prs in all_prs.values())
        logger.info("Found %d open PRs across %d repos", total, len(all_prs))
        return all_prs

    async def _drain_merge_queue(self, all_prs: dict[str, list[PRInfo]]) -> None:
        """Merge all PRs that are ready."""
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
            success = await merge_pr(pr, repo_path)
            if success:
                logger.info("Merged PR #%d in %s", pr.number, pr.repo)
                await sync_after_merge(repo_path)
            else:
                logger.warning("Failed to merge PR #%d in %s", pr.number, pr.repo)

    async def _review_pending(self, all_prs: dict[str, list[PRInfo]]) -> None:
        """Review PRs that need review."""
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
        """Discover work and spawn agents."""
        items = discover_all_repos(repo_paths)
        added = merge_work_items(self.state, items)
        self.state.last_discovery = datetime.now(UTC)
        logger.info("Discovered %d new work items (queue: %d)", added, len(self.state.work_queue))

        available_slots = self.config.max_parallel_agents - len(self.state.active_runs)
        if available_slots <= 0 or not self.state.work_queue:
            return

        batch = self.state.work_queue[:available_slots]
        self.state.work_queue = self.state.work_queue[available_slots:]

        for task in batch:
            self.state.active_runs.append(AgentRun(task=task))

        self._persist()

        results = await run_parallel_agents(
            batch, self.config.default_model, self.config.max_parallel_agents
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
    """Entry point for the orchestrator daemon."""
    orch = Orchestrator(config=config, state_path=state_path)
    await orch.run()
