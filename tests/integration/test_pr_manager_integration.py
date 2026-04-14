"""Integration tests for pr_manager combining forge and models."""

from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

import pytest

from radioactive_ralph.models import PRStatus
from radioactive_ralph.pr_manager import scan_repo


@pytest.mark.asyncio
async def test_scan_repo_integration(mocker) -> None:
    """Test scanning a repository and classifying PRs with mocked forge."""
    repo_path = Path("/tmp/fake-repo")

    # 1. Mock GitClient
    mock_git = mocker.MagicMock()
    mock_git.get_remote_url = mocker.AsyncMock(return_value="https://github.com/org/repo.git")
    mocker.patch("radioactive_ralph.pr_manager.GitClient", return_value=mock_git)

    # 2. Mock ForgeClient
    from radioactive_ralph.forge.base import CIState, ForgeCI, ForgeInfo, ForgePR

    info = ForgeInfo(
        host="github.com",
        slug="org/repo",
        forge_type="github",
        api_base_url="https://api.github.com",
    )

    mock_forge = mocker.MagicMock()
    mock_forge.info = info
    # list_prs returns one PR
    f_pr = ForgePR(
        number=10,
        title="Fix bug",
        author="alice",
        branch="bugfix",
        head_sha="sha1",
        is_draft=False,
        url="http://gh/10",
        updated_at=datetime.now(UTC),
    )
    mock_forge.list_prs = mocker.AsyncMock(return_value=[f_pr])
    # get_pr_ci returns success
    mock_forge.get_pr_ci = mocker.AsyncMock(return_value=ForgeCI(state=CIState.SUCCESS))
    # get_pr_reviews returns approved
    f_pr_approved = f_pr
    f_pr_approved.review_approved = True
    f_pr_approved.review_count = 1
    mock_forge.get_pr_reviews = mocker.AsyncMock(return_value=f_pr_approved)

    # Mock get_forge_client to return our mock_forge
    # It's an async context manager, so we need to mock __aenter__
    mock_forge.__aenter__ = mocker.AsyncMock(return_value=mock_forge)
    mock_forge.__aexit__ = mocker.AsyncMock(return_value=None)
    mocker.patch("radioactive_ralph.pr_manager.get_forge_client", return_value=mock_forge)

    # 3. Run scan
    prs = await scan_repo(repo_path)

    assert len(prs) == 1
    pr = prs[0]
    assert pr.number == 10
    assert pr.status == PRStatus.MERGE_READY
    assert pr.ci_passed is True
    assert pr.is_mergeable is True
