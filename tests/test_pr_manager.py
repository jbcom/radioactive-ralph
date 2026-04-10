"""Tests for PR manager."""

from __future__ import annotations

from pathlib import Path

import pytest

from radioactive_ralph.models import PRInfo, PRStatus
from radioactive_ralph.pr_manager import extract_pr_url


def test_extract_pr_url_found() -> None:
    output = "Created PR at https://github.com/org/repo/pull/42 successfully"
    assert extract_pr_url(output) == "https://github.com/org/repo/pull/42"


def test_extract_pr_url_not_found() -> None:
    assert extract_pr_url("no url here") is None


def test_pr_info_mergeable_requires_ci() -> None:
    pr = PRInfo(
        repo="test/repo", number=1, title="feat", author="bot",
        branch="feat/x", status=PRStatus.MERGE_READY, ci_passed=False
    )
    assert pr.is_mergeable is False


def test_pr_info_mergeable_when_ready() -> None:
    pr = PRInfo(
        repo="test/repo", number=2, title="feat", author="bot",
        branch="feat/y", status=PRStatus.MERGE_READY, ci_passed=True, is_draft=False
    )
    assert pr.is_mergeable is True


@pytest.mark.asyncio
async def test_classify_pr_uses_gh(mocker: pytest.MonkeyPatch, tmp_path: Path) -> None:
    """classify_pr calls gh CLI — mock run_gh with pytest-mock."""
    import json

    mock_run_gh = mocker.patch(
        "radioactive_ralph.pr_manager.run_gh",
        side_effect=[
            ('{"checks":[]}', 0),  # gh pr checks
            (json.dumps({"reviews": [], "reviewRequests": []}), 0),  # gh pr view reviews
        ],
    )

    from radioactive_ralph.pr_manager import classify_pr

    pr = PRInfo(
        repo="org/repo", number=5, title="test", author="bot",
        branch="feat/z", status=PRStatus.NEEDS_REVIEW
    )
    await classify_pr(pr, tmp_path)
    assert mock_run_gh.call_count == 2
