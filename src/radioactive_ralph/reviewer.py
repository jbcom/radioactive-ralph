"""AI-powered code review using the Anthropic SDK.

Reviews PR diffs using Claude and returns structured findings categorised
by severity (error, warning, suggestion, nitpick).

Typical usage::

    async with get_forge_client(url) as forge:
        result = await review_pr(pr_info, repo_path, forge)
        if result.has_blocking_issues:
            print("PR has errors that must be fixed")
"""

from __future__ import annotations

import contextlib
import json
import logging
from pathlib import Path
from typing import TYPE_CHECKING

import anthropic

from radioactive_ralph.models import PRInfo, ReviewFinding, ReviewResult, ReviewSeverity

if TYPE_CHECKING:
    from radioactive_ralph.forge.base import ForgeClient

logger = logging.getLogger(__name__)

REVIEW_SYSTEM_PROMPT = """\
You are a senior code reviewer.
Review the following PR diff and return structured findings.

Rules:
1. ONLY return valid JSON. No conversational filler.
2. Categorise findings by severity: error, warning, suggestion, nitpick.
3. Be concise but helpful.

Format:
{
  "approved": true/false,
  "summary": "Overall summary of the review",
  "findings": [
    {
      "severity": "error",
      "file": "path/to/file.py",
      "line": 42,
      "issue": "Detailed description of the issue",
      "fix": "How to fix it"
    }
  ]
}
"""


async def get_pr_diff(pr: PRInfo, forge: ForgeClient) -> str | None:
    """Fetch the unified diff for a PR via the forge client.

    Args:
        pr: The PR metadata.
        forge: The forge client to use for fetching.

    Returns:
        Raw unified diff string, or None if the fetch failed.
    """
    try:
        # Convert PRInfo back to a lightweight ForgePR for the client
        from radioactive_ralph.forge.base import ForgePR
        f_pr = ForgePR(
            number=pr.number,
            title=pr.title,
            author=pr.author,
            branch=pr.branch,
            head_sha="",  # Not needed for diff
            is_draft=pr.is_draft,
            url=pr.url,
            updated_at=pr.updated_at,
        )
        return await forge.get_pr_diff(f_pr)
    except Exception as e:
        logger.warning("Failed to get diff for PR #%d: %s", pr.number, e, exc_info=True)
        return None


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
    Validates that each finding is a dictionary before access.

    Args:
        raw: Raw text response from the Anthropic API.

    Returns:
        Tuple of (approved, summary, list_of_findings).
    """
    cleaned = raw.strip()

    # Try to extract content between ```json and ```
    if "```json" in cleaned:
        with contextlib.suppress(IndexError):
            cleaned = cleaned.split("```json")[1].split("```")[0]
    elif "```" in cleaned:
        with contextlib.suppress(IndexError):
            cleaned = cleaned.split("```")[1].split("```")[0]

    try:
        data = json.loads(cleaned.strip())
    except json.JSONDecodeError:
        logger.warning("Failed to parse review response as JSON: %r", cleaned)
        return False, "Failed to parse review", []

    approved = data.get("approved", False)
    summary = data.get("summary", "")
    findings: list[ReviewFinding] = []

    for raw_finding in data.get("findings", []):
        if not isinstance(raw_finding, dict):
            continue

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
    forge: ForgeClient,
    model: str = "claude-haiku-4-5-20251001",
    client: anthropic.AsyncAnthropic | None = None,
) -> ReviewResult:
    """Review a PR using the Anthropic API and return structured findings.

    Fetches the PR diff via the provided ForgeClient, sends it to Claude
    with a structured review prompt, and parses the JSON response into
    a :class:`ReviewResult`.

    Args:
        pr: The PR to review.
        repo_path: Local path to the repository.
        forge: Forge client for fetching the diff.
        model: Anthropic model ID to use (default: haiku for cost).
        client: Optional pre-constructed AsyncAnthropic client.

    Returns:
        Structured review result with findings and overall approval status.
    """
    if client is None:
        client = anthropic.AsyncAnthropic()

    diff = await get_pr_diff(pr, forge)
    if diff is None:
        return ReviewResult(
            pr=pr,
            approved=False,
            summary="Failed to fetch PR diff — cannot review. Failing closed.",
        )

    if not diff.strip():
        return ReviewResult(pr=pr, approved=True, summary="Empty diff — nothing to review")

    user_prompt = build_review_prompt(pr, diff)

    response = await client.messages.create(
        model=model,
        max_tokens=4096,
        system=REVIEW_SYSTEM_PROMPT,
        messages=[{"role": "user", "content": user_prompt}],
    )

    # Extract text from the response content blocks
    raw_text = ""
    if response.content:
        for block in response.content:
            # Handle both real TextBlock and MagicMock with 'text' attribute
            if hasattr(block, "text"):
                raw_text += block.text
            elif isinstance(block, dict) and "text" in block:
                raw_text += block["text"]

    approved, summary, findings = parse_review_response(raw_text)

    return ReviewResult(
        pr=pr,
        findings=findings,
        approved=approved,
        summary=summary,
    )


async def batch_review(
    prs: list[tuple[PRInfo, str | Path, ForgeClient]],
    model: str = "claude-haiku-4-5-20251001",
) -> list[ReviewResult]:
    """Review multiple PRs sequentially (API rate-limit friendly).

    Args:
        prs: List of (PRInfo, repo_path, forge_client) tuples to review.
        model: Anthropic model ID to use for all reviews.

    Returns:
        List of ReviewResult in the same order as input.
    """
    api_client = anthropic.AsyncAnthropic()
    results: list[ReviewResult] = []

    for pr, repo_path, forge in prs:
        result = await review_pr(pr, repo_path, forge, model=model, client=api_client)
        results.append(result)

    return results
