"""High-level management of pull requests across multiple repositories.

Orchestrates forge detection, PR discovery, CI/review classification,
and merge operations.
"""

from __future__ import annotations

import asyncio
import logging
import re
from pathlib import Path

from radioactive_ralph.forge import ForgePR, get_forge_client
from radioactive_ralph.git_client import GitClient
from radioactive_ralph.models import PRInfo, PRStatus

logger = logging.getLogger(__name__)


def extract_pr_url(text: str) -> str | None:
    """Extract the first pull/merge request URL from a block of text.

    Supports GitHub, GitLab (including subgroups), and Gitea URL patterns.

    Args:
        text: Raw text output (e.g. from a Claude Code agent run).

    Returns:
        The first matching URL, or None if not found.
    """
    match = re.search(
        r"https?://[^\s/]+/.+?/(?:pull|pulls|merge_requests)/\d+",
        text,
    )
    return match.group(0) if match else None


def pr_to_model(pr: ForgePR, repo_slug: str) -> PRInfo:
    """Convert a forge-neutral ForgePR to the internal PRInfo model.

    Args:
        pr: Normalised PR from a forge client.
        repo_slug: The ``org/repo`` slug used for display.

    Returns:
        A :class:`~radioactive_ralph.models.PRInfo` instance.
    """
    status = PRStatus.NEEDS_REVIEW
    if pr.is_draft:
        status = PRStatus.UNKNOWN
    elif pr.changes_requested:
        status = PRStatus.CHANGES_REQUESTED
    elif pr.review_approved:
        status = PRStatus.MERGE_READY

    return PRInfo(
        repo=repo_slug,
        number=pr.number,
        title=pr.title,
        author=pr.author,
        branch=pr.branch,
        url=pr.url,
        status=status,
        updated_at=pr.updated_at,
        ci_passed=(pr.ci.passed if pr.ci else False),
        is_draft=pr.is_draft,
        review_count=pr.review_count,
    )


async def scan_all_repos(repo_paths: list[Path]) -> dict[str, list[PRInfo]]:
    """Scan multiple repositories for open pull requests.

    Args:
        repo_paths: List of local paths to git repositories.

    Returns:
        Mapping of repo path (string) to list of discovered PRs.
    """
    results = await asyncio.gather(*[scan_repo(p) for p in repo_paths])
    return {str(p): prs for p, prs in zip(repo_paths, results, strict=False)}


async def scan_repo(repo_path: Path) -> list[PRInfo]:
    """Scan a single repository for pull requests via its remote URL.

    Args:
        repo_path: Local path to the repository.

    Returns:
        List of PR metadata for open pull requests.
    """
    git = GitClient(repo_path)
    remote_url = await git.get_remote_url()
    if not remote_url:
        return []

    try:
        async with get_forge_client(remote_url) as forge:
            prs = await forge.list_prs(state="open")

            # Fetch CI and Review status for each PR concurrently
            async def hydrate(p: ForgePR) -> PRInfo:
                ci_task = forge.get_pr_ci(p)
                review_task = forge.get_pr_reviews(p)
                p.ci, _ = await asyncio.gather(ci_task, review_task)
                return pr_to_model(p, forge.info.slug)

            return await asyncio.gather(*[hydrate(p) for p in prs])

    except Exception as e:
        logger.error("Failed to scan PRs for %s: %s", repo_path, e)
        return []


async def merge_pr(pr: PRInfo, repo_path: Path) -> bool:
    """Squash-merge a PR via its forge and sync local repo.

    Args:
        pr: The PR to merge.
        repo_path: Local path to the repository.

    Returns:
        True if merge succeeded, False otherwise.
    """
    git = GitClient(repo_path)
    remote_url = await git.get_remote_url()
    if not remote_url:
        return False

    try:
        async with get_forge_client(remote_url) as forge:
            # Reconstruct ForgePR for the client
            from radioactive_ralph.forge.base import ForgePR

            f_pr = ForgePR(
                number=pr.number,
                title=pr.title,
                author=pr.author,
                branch=pr.branch,
                head_sha="",
                is_draft=pr.is_draft,
                url=pr.url,
                updated_at=pr.updated_at,
            )

            success = await forge.merge_pr(f_pr)
            if success:
                # Sync local repo
                await git.pull()
            return success

    except Exception as e:
        logger.error("Failed to merge PR #%d in %s: %s", pr.number, repo_path, e)
        return False
