"""Main orchestration loop — the daemon that drives everything."""

from __future__ import annotations

import asyncio
import contextlib
import logging
import signal
from datetime import UTC, datetime
from pathlib import Path

from radioactive_ralph.agent_runner import run_parallel_agents
from radioactive_ralph.config import RadioactiveRalphConfig, load_config
from radioactive_ralph.forge import get_forge_client
from radioactive_ralph.models import (
    PRInfo,
    PRStatus,
)
from radioactive_ralph.pr_manager import merge_pr, scan_all_repos
from radioactive_ralph.ralph_says import Variant, ralph_panel, ralph_says
from radioactive_ralph.reviewer import review_pr
from radioactive_ralph.state import load_state, merge_work_items, prune_completed, save_state
from radioactive_ralph.work_discovery import discover_all_repos

logger = logging.getLogger(__name__)


class Orchestrator:
    """Autonomous development orchestrator — the main daemon.

    Runs a continuous loop of scanning PRs, reviewing them, merging if ready,
    discovering new work, and spawning agents to fulfill work items.
    """

    def __init__(
        self,
        config: RadioactiveRalphConfig | None = None,
        variant: Variant = Variant.SAVAGE,
    ):
        """Initialize the orchestrator.

        Args:
            config: Configuration object. If omitted, loaded from disk/env.
            variant: Which 'Ralph' persona to use for feedback.
        """
        self.config = config or load_config()
        self.state = load_state(self.config.resolve_state_path())
        self.variant = variant
        self._stop_event = asyncio.Event()

    async def run(self) -> None:
        """Run the continuous orchestration loop until stopped.

        Args:
            None

        Returns:
            None
        """
        # Handle termination signals
        loop = asyncio.get_running_loop()
        for sig in (signal.SIGINT, signal.SIGTERM):
            loop.add_signal_handler(sig, self.stop)

        ralph_says(self.variant, "startup")

        try:
            while not self._stop_event.is_set():
                with ralph_panel(self.variant, "Cycle Starting"):
                    await self._step()

                # Sleep until next cycle
                with contextlib.suppress(asyncio.TimeoutError):
                    await asyncio.wait_for(
                        self._stop_event.wait(),
                        timeout=self.config.cycle_sleep_seconds,
                    )
        finally:
            save_state(self.state, self.config.resolve_state_path())
            ralph_says(self.variant, "shutdown")

    def stop(self) -> None:
        """Signal the orchestrator to stop gracefully.

        Args:
            None

        Returns:
            None
        """
        self._stop_event.set()

    async def _step(self) -> None:
        """Perform a single iteration of the orchestration loop.

        Args:
            None

        Returns:
            None
        """
        self.state.cycle_count += 1
        repo_paths = self.config.all_repo_paths()

        # 1. Scan for pull requests
        all_prs = await scan_all_repos(repo_paths)
        self.state.last_scan = datetime.now(UTC)

        # 2. Merge ready PRs
        await self._merge_ready(all_prs)

        # 3. Review pending PRs
        await self._review_pending(all_prs)

        # 4. Discover new work
        if self._should_discover():
            work_items = await discover_all_repos(repo_paths)
            self.state.work_queue = merge_work_items(self.state.work_queue, work_items)
            self.state.last_discovery = datetime.now(UTC)

        # 5. Clean up completed runs
        self.state.active_runs = prune_completed(self.state.active_runs)

        # 6. Spawn agents for queued work
        if len(self.state.active_runs) < self.config.max_parallel_agents:
            new_runs = await run_parallel_agents(
                self.state.work_queue,
                self.config,
                max_spawn=self.config.max_parallel_agents - len(self.state.active_runs),
            )
            self.state.active_runs.extend(new_runs)

    async def _merge_ready(self, all_prs: dict[str, list[PRInfo]]) -> None:
        """Merge PRs that are approved and passed CI.

        Args:
            all_prs: Mapping of repo path to list of PR metadata.

        Returns:
            None
        """
        ready = [
            (repo_path, pr)
            for repo_path, prs in all_prs.items()
            for pr in prs
            if pr.is_mergeable
        ]

        for repo_path, pr in ready:
            ralph_says(self.variant, "merging", pr=pr.number, repo=pr.repo)
            success = await merge_pr(pr, Path(repo_path))
            if success:
                ralph_says(self.variant, "merged", pr=pr.number, repo=pr.repo)
            else:
                ralph_says(self.variant, "merge_failed", pr=pr.number, repo=pr.repo)

    async def _review_pending(self, all_prs: dict[str, list[PRInfo]]) -> None:
        """Review PRs that need review.

        Args:
            all_prs: Mapping of repo path to list of PR metadata.

        Returns:
            None
        """
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

            # Use ForgeClient to fetch diff for review
            async with get_forge_client(pr.url) as forge:
                result = await review_pr(
                    pr, repo_path, forge, model=self.config.bulk_model
                )

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

    def _should_discover(self) -> bool:
        """Return True if it's time to run work discovery again.

        Returns:
            True if enough time has passed since last discovery.
        """
        if not self.state.last_discovery:
            return True
        delta = datetime.now(UTC) - self.state.last_discovery
        return delta.total_seconds() > 3600  # hourly discovery
