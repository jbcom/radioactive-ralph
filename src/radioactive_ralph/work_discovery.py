"""Heuristics and logic for finding new work to do in a repository."""

from __future__ import annotations

import logging
import uuid
from pathlib import Path

from radioactive_ralph.models import WorkItem, WorkPriority

logger = logging.getLogger(__name__)


async def discover_all_repos(repo_paths: list[Path]) -> list[WorkItem]:
    """Scan multiple repositories for new work.

    Args:
        repo_paths: List of local paths to git repositories.

    Returns:
        Flattened list of all discovered work items.
    """
    all_work = []
    for path in repo_paths:
        try:
            items = await discover_work(path)
            all_work.extend(items)
        except Exception as e:
            logger.error("Failed to discover work for %s: %s", path, e)
    return all_work


async def discover_work(repo_path: Path) -> list[WorkItem]:
    """Scan a single repository for missing documentation, tests, or features.

    Args:
        repo_path: Local path to the git repository.

    Returns:
        List of discovered work items.
    """
    work_items = []

    # 1. Check for missing architecture docs
    if not (repo_path / "docs" / "ARCHITECTURE.md").exists():
        work_items.append(
            WorkItem(
                id=str(uuid.uuid4()),
                repo_path=str(repo_path),
                description="Create ARCHITECTURE.md to document system design",
                priority=WorkPriority.MEDIUM,
                source="discovery/docs",
            )
        )

    # 2. Check for missing tests directory
    if not (repo_path / "tests").is_dir():
        work_items.append(
            WorkItem(
                id=str(uuid.uuid4()),
                repo_path=str(repo_path),
                description="Scaffold initial test suite in tests/",
                priority=WorkPriority.HIGH,
                source="discovery/tests",
            )
        )

    # 3. Check for TODOs in code (simple heuristic)
    # In a real implementation, we would use ripgrep or AST analysis

    return work_items
