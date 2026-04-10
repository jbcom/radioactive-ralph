"""PR management — forge-agnostic orchestration of pull/merge requests.

This module ties together the :mod:`forge` abstraction and the local
:class:`~radioactive_ralph.git_client.GitClient` to provide high-level
PR lifecycle operations:

- Scanning all repos for open PRs
- Classifying PRs (CI state, review state, staleness)
- Merging ready PRs
- Extracting PR URLs from agent output

The forge client is auto-detected from each repo's git remote URL, so
GitHub, GitLab, Gitea, and Forgejo are all supported transparently.

Example::

    repo_paths = [Path("/srv/projects/my-app")]
    pr_map = await scan_all_repos(repo_paths)
    for repo, prs in pr_map.items():
        for pr in prs:
            if pr.is_mergeable:
                forge = get_forge_client(...)
                await forge.merge_pr(pr)
"""

from __future__ import annotations

import asyncio
import logging
import re
from pathlib import Path

from .forge import ForgeClient, ForgePR, get_forge_client
from .git_client import GitClient
from .models import PRInfo, PRStatus

logger = logging.getLogger(__name__)


def extract_pr_url(text: str) -> str | None:
    """Extract the first GitHub-style PR URL from a block of text.

    Searches for URLs matching ``https://<host>/<org>/<repo>/pull/<n>``
    or the GitLab equivalent ``/merge_requests/<n>``.

    Args:
        text: Raw text output (e.g. from a Claude Code agent run).

    Returns:
        The first matching URL, or None if not found.
    """
    match = re.search(
        r"https://[^\s/]+/[^/\s]+/[^/\s]+/(?:pull|merge_requests)/\d+",
        text,
    )
    return match.group(0) if match else None


def _forge_pr_to_model(pr: ForgePR, repo_slug: str) -> PRInfo:
    """Convert a forge-neutral ForgePR to the internal PRInfo model.

    Args:
        pr: Normalised PR from a forge client.
        repo_slug: The ``org/repo`` slug used for display.

    Returns:
        A :class:`~radioactive_ralph.models.PRInfo` instance.
    """
    status = PRStatus.NEEDS_REVIEW
    if pr.is_draft:
        status = PRStatus.IN_PROGRESS
    elif pr.is_stale:
        status = PRStatus.STALE
    elif pr.changes_requested:
        status = PRStatus.NEEDS_FIXES
    elif pr.ci is not None and pr.ci.failed:
        status = PRStatus.CI_FAILING
    elif pr.review_approved and pr.ci is not None and pr.ci.passed:
        status = PRStatus.MERGE_READY

    return PRInfo(
        repo=repo_slug,
        number=pr.number,
        title=pr.title,
        author=pr.author,
        branch=pr.branch,
        status=status,
        ci_passed=pr.ci.passed if pr.ci else False,
        review_count=pr.review_count,
        has_unresolved_comments=pr.changes_requested,
        is_draft=pr.is_draft,
        updated_at=pr.updated_at,
        url=pr.url,
    )


async def classify_pr(pr: ForgePR, forge: ForgeClient) -> ForgePR:
    """Classify a PR by fetching CI and review state from the forge.

    Short-circuits early for drafts and stale PRs to avoid unnecessary API
    calls.

    Args:
        pr: The PR to classify (mutated in place).
        forge: An open forge client for the repo.

    Returns:
        The same PR object with ``ci`` and review fields populated.
    """
    if pr.is_draft or pr.is_stale:
        return pr

    pr.ci = await forge.get_pr_ci(pr)
    if pr.ci.failed:
        return pr  # no point checking reviews

    pr = await forge.get_pr_reviews(pr)
    return pr


async def _classify_with_forge(pr: ForgePR, forge: ForgeClient) -> ForgePR:
    """Classify a single PR, logging and swallowing errors.

    Args:
        pr: The PR to classify.
        forge: An open forge client.

    Returns:
        The (possibly partially classified) PR.
    """
    try:
        return await classify_pr(pr, forge)
    except Exception as e:
        logger.warning("Failed to classify PR #%d: %s", pr.number, e)
        return pr


async def scan_repo(repo_path: Path) -> list[PRInfo]:
    """Scan a single repo for open PRs and classify each one.

    Auto-detects the forge from the git remote URL. Returns an empty list
    if no remote is found or the forge cannot be contacted.

    Args:
        repo_path: Absolute path to a local git repository.

    Returns:
        Classified :class:`~radioactive_ralph.models.PRInfo` objects.
    """
    git = GitClient(repo_path)
    remote_url = await git.get_remote_url()
    if not remote_url:
        logger.warning("No remote URL found for %s — skipping", repo_path)
        return []

    try:
        forge_client = get_forge_client(remote_url)
    except (ValueError, Exception) as e:
        logger.warning("Cannot create forge client for %s: %s", repo_path, e)
        return []

    try:
        async with forge_client as forge:
            raw_prs = await forge.list_prs()
            classified = await asyncio.gather(
                *(_classify_with_forge(pr, forge) for pr in raw_prs)
            )
            slug = forge.info.slug
            return [_forge_pr_to_model(pr, slug) for pr in classified]
    except Exception as e:
        logger.error("Failed to scan PRs for %s: %s", repo_path, e)
        return []


async def scan_all_repos(repo_paths: list[Path]) -> dict[str, list[PRInfo]]:
    """Scan all repos for open PRs and classify them concurrently.

    Each repo gets its own forge client, so different forges in the same
    scan are handled correctly.

    Args:
        repo_paths: List of local repository paths to scan.

    Returns:
        Mapping of repo path string → list of classified PRs.
    """
    results = await asyncio.gather(*(scan_repo(p) for p in repo_paths))
    return {str(p): prs for p, prs in zip(repo_paths, results, strict=False)}


async def merge_pr(info: PRInfo, repo_path: Path) -> bool:
    """Merge a PR that is MERGE_READY.

    Detects the forge from the repo's git remote and delegates to the
    forge-specific merge implementation (squash merge + branch deletion).

    Args:
        info: The PR to merge (must have ``repo`` slug and ``number``).
        repo_path: Local path to the repository.

    Returns:
        True if merged successfully, False on error.
    """
    git = GitClient(repo_path)
    remote_url = await git.get_remote_url()
    if not remote_url:
        logger.error("No remote URL for %s — cannot merge", repo_path)
        return False

    try:
        forge_client = get_forge_client(remote_url)
    except Exception as e:
        logger.error("Cannot create forge client: %s", e)
        return False

    # Construct a minimal ForgePR for the merge call
    pr = ForgePR(
        number=info.number,
        title=info.title,
        author=info.author,
        branch=info.branch,
        head_sha="",
        is_draft=info.is_draft,
        url=info.url,
        updated_at=info.updated_at,
    )

    async with forge_client as forge:
        ok = await forge.merge_pr(pr)

    if ok:
        await git.pull()

    return ok
