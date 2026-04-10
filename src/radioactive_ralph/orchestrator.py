"""Main orchestration loop — the daemon that drives everything."""

from __future__ import annotations

import asyncio
import contextlib
import logging
import signal
from datetime import UTC, datetime
from pathlib import Path

from .agent_runner import run_parallel_agents
from .config import RadioactiveRalphConfig, load_config
from .models import (
    AgentRun,
    PRInfo,
    PRStatus,
)
from .pr_manager import merge_pr, scan_all_repos
from .ralph_says import Variant, ralph_panel, ralph_says
from .reviewer import review_pr
from .state import load_state, merge_work_items, prune_completed, save_state
from .work_discovery import discover_all_repos

logger = logging.getLogger(__name__)


class Orchestrator:
    """Autonomous development orchestrator — the main daemon."""

    def __init__(
        self,
        config: RadioactiveRalphConfig | None = None,
        state_path: Path | None = None,
        variant: Variant = Variant.GREEN,
    ):
        self.config = config or load_config()
        self.state_path = state_path or self.config.resolve_state_path()
        self.state = load_state(self.state_path)
        self.variant = variant
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
        logger.debug("Shutdown requested — finishing active agents...")
        ralph_says(self.variant, "warning")
        self._shutdown.set()

    def _persist(self) -> None:
        save_state(self.state, self.state_path)

    async def run(self) -> None:
        """Main loop — scan, merge, review, discover, execute, repeat."""
        ralph_panel(self.variant, "startup", title=f"radioactive-ralph · {self.variant.value}")
        logger.debug(
            "Orchestrator starting with %d configured orgs", len(self.config.orgs)
        )

        while not self._shutdown.is_set():
            self.state.cycle_count += 1
            ralph_says(self.variant, "cycle_start", cycle=self.state.cycle_count)

            try:
                await self._cycle()
            except Exception:
                logger.exception("Error in orchestration cycle")
                ralph_says(self.variant, "error")

            self._persist()

            sleep_seconds = self.config.cycle_sleep_seconds
            ralph_says(self.variant, "sleeping", seconds=sleep_seconds)
            with contextlib.suppress(TimeoutError):
                await asyncio.wait_for(
                    self._shutdown.wait(), timeout=float(sleep_seconds)
                )

        ralph_panel(self.variant, "shutdown", cycles=self.state.cycle_count)
        self._persist()

    async def _cycle(self) -> None:
        """Execute one full orchestration cycle."""
        repo_paths = self.config.all_repo_paths()
        if not repo_paths:
            ralph_says(self.variant, "warning")
            logger.debug("No repos configured — nothing to do")
            return

        all_prs = await self._scan_prs(repo_paths)
        await self._drain_merge_queue(all_prs)
        await self._review_pending(all_prs)
        await self._discover_and_execute(repo_paths)

        prune_completed(self.state, keep=100)

    async def _scan_prs(self, repo_paths: list[Path]) -> dict[str, list[PRInfo]]:
        """Scan all repos for PRs."""
        ralph_says(self.variant, "scanning", count=len(repo_paths))
        all_prs = await scan_all_repos(repo_paths)
        self.state.last_scan = datetime.now(UTC)

        total = sum(len(prs) for prs in all_prs.values())
        ralph_says(self.variant, "scan_done", total=total, repos=len(all_prs))
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

        for repo_path, pr in merge_ready:
            ralph_says(self.variant, "merging", pr=pr.number, repo=pr.repo)
            success = await merge_pr(pr, Path(repo_path))
            if success:
                ralph_says(self.variant, "merged", pr=pr.number, repo=pr.repo)
            else:
                ralph_says(self.variant, "merge_failed", pr=pr.number, repo=pr.repo)

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

        for repo_path, pr in needs_review:
            ralph_says(self.variant, "reviewing", pr=pr.number, repo=pr.repo)
            result = await review_pr(pr, repo_path, model=self.config.bulk_model)
            if result.approved:
                ralph_says(
                    self.variant, "reviewed_approved", pr=pr.number, repo=pr.repo
                )
            else:
                ralph_says(
                    self.variant,
                    "reviewed_changes",
                    pr=pr.number,
                    repo=pr.repo,
                    count=len(result.findings),
                )

    async def _discover_and_execute(self, repo_paths: list[Path]) -> None:
        """Discover work and spawn agents."""
        ralph_says(self.variant, "discovering", count=len(repo_paths))
        items = discover_all_repos(repo_paths)
        added = merge_work_items(self.state, items)
        self.state.last_discovery = datetime.now(UTC)
        ralph_says(
            self.variant,
            "discovered",
            added=added,
            total=len(self.state.work_queue),
        )

        available_slots = self.config.max_parallel_agents - len(self.state.active_runs)
        if available_slots <= 0 or not self.state.work_queue:
            if not self.state.work_queue:
                ralph_says(self.variant, "no_work")
            return

        batch = self.state.work_queue[:available_slots]
        self.state.work_queue = self.state.work_queue[available_slots:]

        for task in batch:
            self.state.active_runs.append(AgentRun(task=task))

        self._persist()

        ralph_says(self.variant, "executing", count=len(batch))
        results = await run_parallel_agents(
            batch,
            self.config.default_model,
            self.config.agent_timeout_minutes * 60,
            config=self.config,
        )

        succeeded = 0
        failed = 0
        for result in results:
            for run in self.state.active_runs:
                if run.task.id == result.task_id:
                    run.result = result
                    self.state.completed_runs.append(run)
                    break

            if result.pr_url:
                succeeded += 1
                ralph_says(self.variant, "pr_created", url=result.pr_url)
            else:
                failed += 1
                ralph_says(
                    self.variant, "agent_failed", task=result.task_id or "unknown"
                )

        ralph_says(
            self.variant,
            "agent_done",
            success=succeeded,
            failed=failed,
            total=len(results),
        )

        self.state.active_runs = [r for r in self.state.active_runs if r.is_active]


async def start_orchestrator(
    config: RadioactiveRalphConfig | None = None,
    state_path: Path | None = None,
) -> None:
    """Entry point for the orchestrator daemon."""
    orch = Orchestrator(config=config, state_path=state_path)
    await orch.run()
