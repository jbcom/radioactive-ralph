"""AI-powered code review using the Anthropic SDK.

Reviews PR diffs using Claude and returns structured findings categorised
by severity (error, warning, suggestion, nitpick).

Typical usage::

    result = await review_pr(pr_info, repo_path="/srv/projects/my-app")
    if result.has_blocking_issues:
        print("PR has errors that must be fixed")
"""

from __future__ import annotations

import asyncio
import json
import logging
from pathlib import Path
from typing import TYPE_CHECKING

import anthropic

from .models import PRInfo, ReviewFinding, ReviewResult, ReviewSeverity

if TYPE_CHECKING:
    pass

logger = logging.getLogger(__name__)

REVIEW_SYSTEM_PROMPT = """\
You are a senior code reviewer. Review the following PR diff and return structured findings.

Rules:
- Focus on correctness, security, performance, and maintainability
- Be specific about file and line numbers
- For each issue, provide a concrete fix
- If the code is acceptable, return an empty findings list
- Severity levels: error (must fix), warning (should fix), suggestion (nice to have), nitpick

Return ONLY valid JSON in this format:
{
  "approved": true/false,
  "summary": "Brief overall assessment",
  "findings": [
    {
      "severity": "error|warning|suggestion|nitpick",
      "file": "path/to/file",
      "line": 42,
      "issue": "What's wrong",
      "fix": "How to fix it"
    }
  ]
}
"""


async def get_pr_diff(pr_number: int, repo_path: str | Path) -> str:
    """Fetch the unified diff for a PR via the gh CLI.

    Uses ``asyncio.create_subprocess_exec`` (no shell, no injection risk).
    Falls back to an empty string on any error so callers can handle it
    gracefully.

    Args:
        pr_number: The PR/MR number (integer).
        repo_path: Local path to the repository.

    Returns:
        Raw unified diff string, or empty string on error.
    """
    try:
        proc = await asyncio.create_subprocess_exec(
            "gh", "pr", "diff", str(pr_number),
            cwd=str(repo_path),
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout, _ = await proc.communicate()
        return stdout.decode("utf-8", errors="replace")
    except Exception as e:
        logger.warning("Failed to get diff for PR #%d: %s", pr_number, e)
        return ""


def build_review_prompt(pr: PRInfo, diff: str) -> str:
    """Build the review prompt with PR context and diff.

    Args:
        pr: PR metadata (number, title, author, etc.).
        diff: Unified diff content (truncated to 50 000 chars).

    Returns:
        Formatted prompt string for the Anthropic API.
    """
    return f"""\
PR #{pr.number}: {pr.title}
Author: {pr.author}
Branch: {pr.branch}
Repository: {pr.repo}

--- DIFF START ---
{diff[:50000]}
--- DIFF END ---

Review this diff and return structured JSON findings.
"""


def parse_review_response(raw: str) -> tuple[bool, str, list[ReviewFinding]]:
    """Parse the AI review response into structured findings.

    Strips Markdown code fences if present before attempting JSON parse.

    Args:
        raw: Raw text response from the Anthropic API.

    Returns:
        Tuple of (approved, summary, list_of_findings).
    """
    cleaned = raw.strip()
    if cleaned.startswith("```json"):
        cleaned = cleaned[7:]
    if cleaned.startswith("```"):
        cleaned = cleaned[3:]
    if cleaned.endswith("```"):
        cleaned = cleaned[:-3]

    try:
        data = json.loads(cleaned.strip())
    except json.JSONDecodeError:
        logger.warning("Failed to parse review response as JSON")
        return False, "Failed to parse review", []

    approved = data.get("approved", False)
    summary = data.get("summary", "")
    findings: list[ReviewFinding] = []

    for raw_finding in data.get("findings", []):
        try:
            severity = ReviewSeverity(raw_finding.get("severity", "suggestion"))
        except ValueError:
            severity = ReviewSeverity.SUGGESTION

        findings.append(
            ReviewFinding(
                severity=severity,
                file=raw_finding.get("file", "unknown"),
                line=raw_finding.get("line"),
                issue=raw_finding.get("issue", ""),
                fix=raw_finding.get("fix", ""),
            )
        )

    return approved, summary, findings


async def review_pr(
    pr: PRInfo,
    repo_path: str | Path,
    model: str = "claude-haiku-4-5-20251001",
    client: anthropic.Anthropic | None = None,
) -> ReviewResult:
    """Review a PR using the Anthropic API and return structured findings.

    Fetches the PR diff via gh CLI, sends it to Claude with a structured
    review prompt, and parses the JSON response into :class:`ReviewResult`.

    Args:
        pr: The PR to review.
        repo_path: Local path to the repository.
        model: Anthropic model ID to use (default: haiku for cost).
        client: Optional pre-constructed Anthropic client.

    Returns:
        Structured review result with findings and overall approval status.
    """
    if client is None:
        client = anthropic.Anthropic()

    diff = await get_pr_diff(pr.number, repo_path)
    if not diff.strip():
        return ReviewResult(pr=pr, approved=True, summary="Empty diff — nothing to review")

    user_prompt = build_review_prompt(pr, diff)

    response = client.messages.create(
        model=model,
        max_tokens=4096,
        system=REVIEW_SYSTEM_PROMPT,
        messages=[{"role": "user", "content": user_prompt}],
    )

    raw_text = response.content[0].text if response.content else ""
    approved, summary, findings = parse_review_response(raw_text)

    return ReviewResult(
        pr=pr,
        findings=findings,
        approved=approved,
        summary=summary,
    )


async def batch_review(
    prs: list[tuple[PRInfo, str | Path]],
    model: str = "claude-haiku-4-5-20251001",
) -> list[ReviewResult]:
    """Review multiple PRs sequentially (API rate-limit friendly).

    Args:
        prs: List of (PRInfo, repo_path) tuples to review.
        model: Anthropic model ID to use for all reviews.

    Returns:
        List of ReviewResult in the same order as input.
    """
    api_client = anthropic.Anthropic()
    results: list[ReviewResult] = []

    for pr, repo_path in prs:
        result = await review_pr(pr, repo_path, model=model, client=api_client)
        results.append(result)

    return results
