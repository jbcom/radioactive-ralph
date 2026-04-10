"""GitHub PR management via gh CLI — list, classify, merge, get diffs."""

from __future__ import annotations

import asyncio
import json
import re
from datetime import UTC, datetime, timedelta
from pathlib import Path

from .models import PRInfo, PRStatus

STALE_THRESHOLD_DAYS = 7


async def run_gh(args: list[str], cwd: str | Path | None = None) -> tuple[str, int]:
    """Run a gh CLI command and return (stdout, returncode)."""
    proc = await asyncio.create_subprocess_exec(
        "gh",
        *args,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
        cwd=cwd,
    )
    stdout, _ = await proc.communicate()
    return stdout.decode("utf-8", errors="replace"), proc.returncode or 0


async def list_open_prs(repo_path: str | Path) -> list[PRInfo]:
    """List all open PRs for a repository and classify them."""
    fields = "number,title,author,headRefName,isDraft,updatedAt,url"
    stdout, rc = await run_gh(
        ["pr", "list", "--json", fields, "--limit", "100"],
        cwd=repo_path,
    )
    if rc != 0 or not stdout.strip():
        return []

    raw_prs = json.loads(stdout)
    repo_name = Path(repo_path).name
    prs: list[PRInfo] = []

    for raw in raw_prs:
        pr = PRInfo(
            repo=repo_name,
            number=raw["number"],
            title=raw["title"],
            author=raw.get("author", {}).get("login", "unknown"),
            branch=raw["headRefName"],
            is_draft=raw.get("isDraft", False),
            url=raw.get("url", ""),
            status=PRStatus.NEEDS_REVIEW,
        )
        if raw.get("updatedAt"):
            pr.updated_at = datetime.fromisoformat(raw["updatedAt"].replace("Z", "+00:00"))

        prs.append(pr)

    return prs


async def classify_pr(pr: PRInfo, repo_path: str | Path) -> PRInfo:
    """Classify a PR by checking CI status and reviews."""
    if pr.is_draft:
        pr.status = PRStatus.DRAFT
        return pr

    now = datetime.now(UTC)
    if (now - pr.updated_at) > timedelta(days=STALE_THRESHOLD_DAYS):
        pr.status = PRStatus.STALE
        return pr

    ci_stdout, ci_rc = await run_gh(
        ["pr", "checks", str(pr.number), "--json", "state"],
        cwd=repo_path,
    )

    ci_passed = False
    if ci_rc == 0 and ci_stdout.strip():
        raw = json.loads(ci_stdout)
        # gh pr checks --json wraps in {"checks": [...]}
        checks = raw.get("checks", raw) if isinstance(raw, dict) else raw
        if checks:
            ci_passed = all(c.get("state") == "SUCCESS" for c in checks)
            ci_failed = any(c.get("state") == "FAILURE" for c in checks)
            if ci_failed:
                pr.status = PRStatus.CI_FAILING
                pr.ci_passed = False
                return pr

    pr.ci_passed = ci_passed

    review_stdout, review_rc = await run_gh(
        ["pr", "view", str(pr.number), "--json", "reviews,reviewRequests"],
        cwd=repo_path,
    )

    if review_rc == 0 and review_stdout.strip():
        review_data = json.loads(review_stdout)
        reviews = review_data.get("reviews", [])
        pr.review_count = len(reviews)

        has_changes_requested = any(r.get("state") == "CHANGES_REQUESTED" for r in reviews)
        has_approved = any(r.get("state") == "APPROVED" for r in reviews)

        if has_changes_requested:
            pr.status = PRStatus.NEEDS_FIXES
            pr.has_unresolved_comments = True
        elif has_approved and ci_passed:
            pr.status = PRStatus.MERGE_READY
        elif ci_passed:
            pr.status = PRStatus.NEEDS_REVIEW
        else:
            pr.status = PRStatus.NEEDS_REVIEW

    return pr


async def get_pr_diff(pr_number: int, repo_path: str | Path) -> str:
    """Get the full diff for a PR."""
    stdout, _ = await run_gh(["pr", "diff", str(pr_number)], cwd=repo_path)
    return stdout


async def merge_pr(pr: PRInfo, repo_path: str | Path) -> bool:
    """Squash-merge a PR and delete the branch."""
    _, rc = await run_gh(
        ["pr", "merge", str(pr.number), "--squash", "--delete-branch"],
        cwd=repo_path,
    )
    return rc == 0


async def sync_after_merge(repo_path: str | Path) -> bool:
    """Pull latest main after a merge."""
    proc = await asyncio.create_subprocess_exec(
        "git", "pull", "--ff-only",
        cwd=repo_path,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )
    _, _ = await proc.communicate()
    return (proc.returncode or 0) == 0


async def scan_all_repos(repo_paths: list[Path]) -> dict[str, list[PRInfo]]:
    """Scan all repos for open PRs and classify them."""
    results: dict[str, list[PRInfo]] = {}

    for repo_path in repo_paths:
        prs = await list_open_prs(repo_path)
        classified = await asyncio.gather(
            *(classify_pr(pr, repo_path) for pr in prs)
        )
        results[str(repo_path)] = list(classified)

    return results


def extract_pr_url(text: str) -> str | None:
    """Extract a GitHub PR URL from text output."""
    match = re.search(r"https://github\.com/[^/]+/[^/]+/pull/\d+", text)
    return match.group(0) if match else None
