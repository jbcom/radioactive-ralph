"""Main orchestration loop — the daemon that drives everything.

Under rewrite — see docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md.
`Orchestrator.run()` raises NotImplementedError until M2 lands the new daemon
(per-repo supervisor + Unix socket + stream-json session control). Inner helpers
(`_merge_ready`, `_review_pending`, `_should_discover`) are preserved because
their tests remain useful as regression coverage during the rewrite.
"""

from __future__ import annotations

import logging
from datetime import UTC, datetime
from pathlib import Path

from radioactive_ralph.config import RadioactiveRalphConfig, load_config
from radioactive_ralph.forge import get_forge_client
from radioactive_ralph.models import (
    PRInfo,
    PRStatus,
)
from radioactive_ralph.pr_manager import merge_pr
from radioactive_ralph.ralph_says import Variant, ralph_says
from radioactive_ralph.reviewer import review_pr
from radioactive_ralph.state import load_state

logger = logging.getLogger(__name__)

_REWRITE_MSG = (
    "Orchestrator.run is under rewrite — see "
    "docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md (M2). "
    "The replacement is a supervisor process launched via multiplexer "
    "(tmux/screen/setsid), exposing a Unix socket and managing "
    "`claude -p --input-format stream-json` subprocesses."
)


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

    async def run(self) -> None:
        """Run the continuous orchestration loop.

        Under rewrite — raises NotImplementedError. See module docstring + PRD.
        """
        raise NotImplementedError(_REWRITE_MSG)

    def stop(self) -> None:
        """Signal the orchestrator to stop gracefully.

        Under rewrite — raises NotImplementedError. See module docstring + PRD.
        """
        raise NotImplementedError(_REWRITE_MSG)

    async def _merge_ready(self, all_prs: dict[str, list[PRInfo]]) -> None:
        """Merge PRs that are approved and passed CI.

        Args:
            all_prs: Mapping of repo path to list of PR metadata.

        Returns:
            None
        """
        ready = [
            (repo_path, pr) for repo_path, prs in all_prs.items() for pr in prs if pr.is_mergeable
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
                result = await review_pr(pr, repo_path, forge, model=self.config.bulk_model)

            if result.approved:
                ralph_says(self.variant, "reviewed_approved", pr=pr.number, repo=pr.repo)
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
