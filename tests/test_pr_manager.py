"""Tests for PR manager."""

from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

import pytest
from pytest_mock import MockerFixture

from radioactive_ralph.models import PRStatus
from radioactive_ralph.pr_manager import extract_pr_url


def test_extract_pr_url_found() -> None:
    """Test extracting a PR URL from text."""
    output = "Created PR at https://github.com/org/repo/pull/42 successfully"
    assert extract_pr_url(output) == "https://github.com/org/repo/pull/42"


@pytest.mark.asyncio
async def test_classify_pr_logic(mocker: MockerFixture) -> None:
    """Test that forge-neutral PRs are correctly converted to PRInfo with status."""
    from radioactive_ralph.forge.base import ForgePR, ForgeCI, CIState
    from radioactive_ralph.pr_manager import pr_to_model
    
    forge_pr = ForgePR(
        number=5, title="test", author="bot", branch="feat/z",
        head_sha="abc123", is_draft=False, url="",
        updated_at=datetime.now(UTC),
    )
    forge_pr.ci = ForgeCI(state=CIState.SUCCESS)
    forge_pr.review_approved = True
    
    result = pr_to_model(forge_pr, "org/repo")
    
    assert result.status == PRStatus.MERGE_READY
    assert result.ci_passed is True
    assert result.is_mergeable is True
