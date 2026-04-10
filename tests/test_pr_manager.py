"""Tests for PR manager."""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime
from pathlib import Path

import pytest
from pytest_mock import MockerFixture

from radioactive_ralph.forge.base import CIState, ForgeCI, ForgePR
from radioactive_ralph.models import PRStatus
from radioactive_ralph.pr_manager import (
    extract_pr_url,
    merge_pr,
    pr_to_model,
    scan_all_repos,
    scan_repo,
)


def test_extract_pr_url_found() -> None:
    """Test extracting a PR URL from text."""
    output = "Created PR at https://github.com/org/repo/pull/42 successfully"
    assert extract_pr_url(output) == "https://github.com/org/repo/pull/42"

    output = "Merge request https://gitlab.com/org/sub/repo/-/merge_requests/1 is open"
    assert extract_pr_url(output) == "https://gitlab.com/org/sub/repo/-/merge_requests/1"

    assert extract_pr_url("No url here") is None


def test_pr_to_model_status_mapping() -> None:
    """Test that forge-neutral PRs are correctly converted to PRInfo with status."""
    
    # 1. Merge Ready
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

    # 2. Draft
    forge_pr.is_draft = True
    result = pr_to_model(forge_pr, "org/repo")
    assert result.status == PRStatus.UNKNOWN

    # 3. Changes Requested
    forge_pr.is_draft = False
    forge_pr.changes_requested = True
    result = pr_to_model(forge_pr, "org/repo")
    assert result.status == PRStatus.CHANGES_REQUESTED

    # 4. Needs Review (no approvals, no changes requested, not draft)
    forge_pr.changes_requested = False
    forge_pr.review_approved = False
    result = pr_to_model(forge_pr, "org/repo")
    assert result.status == PRStatus.NEEDS_REVIEW


@pytest.fixture
def mock_forge_client(mocker):
    client = mocker.AsyncMock()
    # Support async context manager
    client.__aenter__.return_value = client
    client.__aexit__.return_value = None
    
    # Set info slug
    client.info = mocker.Mock()
    client.info.slug = "org/repo"
    
    return client


@pytest.mark.asyncio
async def test_scan_repo_success(mocker, mock_forge_client):
    mocker.patch("radioactive_ralph.pr_manager.GitClient.get_remote_url", return_value="https://github.com/org/repo")
    mocker.patch("radioactive_ralph.pr_manager.get_forge_client", return_value=mock_forge_client)
    
    pr = ForgePR(number=1, title="t", author="a", branch="b", head_sha="", is_draft=False, url="", updated_at=datetime.now(UTC))
    
    mock_forge_client.list_prs.return_value = [pr]
    mock_forge_client.get_pr_ci.return_value = ForgeCI(state=CIState.SUCCESS)
    
    def modify_pr_review(p):
        p.review_approved = True
        return p
        
    mock_forge_client.get_pr_reviews.side_effect = modify_pr_review

    prs = await scan_repo(Path("/repo"))
    assert len(prs) == 1
    assert prs[0].number == 1
    assert prs[0].ci_passed is True
    assert prs[0].status == PRStatus.MERGE_READY


@pytest.mark.asyncio
async def test_scan_repo_no_remote(mocker):
    mocker.patch("radioactive_ralph.pr_manager.GitClient.get_remote_url", return_value=None)
    prs = await scan_repo(Path("/repo"))
    assert prs == []


@pytest.mark.asyncio
async def test_scan_repo_exception(mocker):
    mocker.patch("radioactive_ralph.pr_manager.GitClient.get_remote_url", return_value="https://github.com/org/repo")
    mocker.patch("radioactive_ralph.pr_manager.get_forge_client", side_effect=Exception("API Error"))
    
    prs = await scan_repo(Path("/repo"))
    assert prs == []


@pytest.mark.asyncio
async def test_scan_all_repos(mocker):
    mock_scan = mocker.patch("radioactive_ralph.pr_manager.scan_repo", side_effect=[
        [mocker.Mock(number=1)],
        [mocker.Mock(number=2)],
    ])
    
    paths = [Path("/repo1"), Path("/repo2")]
    results = await scan_all_repos(paths)
    
    assert list(results.keys()) == ["/repo1", "/repo2"]
    assert len(results["/repo1"]) == 1
    assert len(results["/repo2"]) == 1
    assert mock_scan.call_count == 2


@pytest.mark.asyncio
async def test_merge_pr_success(mocker, mock_forge_client):
    mock_pull = mocker.patch("radioactive_ralph.pr_manager.GitClient.pull", return_value=True)
    mocker.patch("radioactive_ralph.pr_manager.GitClient.get_remote_url", return_value="https://github.com/org/repo")
    mocker.patch("radioactive_ralph.pr_manager.get_forge_client", return_value=mock_forge_client)
    
    mock_forge_client.merge_pr.return_value = True
    
    pr = pr_to_model(ForgePR(number=1, title="t", author="a", branch="b", head_sha="", is_draft=False, url="", updated_at=datetime.now(UTC)), "org/repo")
    
    result = await merge_pr(pr, Path("/repo"))
    assert result is True
    mock_pull.assert_called_once()
    mock_forge_client.merge_pr.assert_called_once()


@pytest.mark.asyncio
async def test_merge_pr_fail(mocker, mock_forge_client):
    mock_pull = mocker.patch("radioactive_ralph.pr_manager.GitClient.pull", return_value=True)
    mocker.patch("radioactive_ralph.pr_manager.GitClient.get_remote_url", return_value="https://github.com/org/repo")
    mocker.patch("radioactive_ralph.pr_manager.get_forge_client", return_value=mock_forge_client)
    
    mock_forge_client.merge_pr.return_value = False
    
    pr = pr_to_model(ForgePR(number=1, title="t", author="a", branch="b", head_sha="", is_draft=False, url="", updated_at=datetime.now(UTC)), "org/repo")
    
    result = await merge_pr(pr, Path("/repo"))
    assert result is False
    mock_pull.assert_not_called()


@pytest.mark.asyncio
async def test_merge_pr_no_remote(mocker):
    mocker.patch("radioactive_ralph.pr_manager.GitClient.get_remote_url", return_value=None)
    pr = pr_to_model(ForgePR(number=1, title="t", author="a", branch="b", head_sha="", is_draft=False, url="", updated_at=datetime.now(UTC)), "org/repo")
    
    result = await merge_pr(pr, Path("/repo"))
    assert result is False


@pytest.mark.asyncio
async def test_merge_pr_exception(mocker):
    mocker.patch("radioactive_ralph.pr_manager.GitClient.get_remote_url", return_value="https://github.com/org/repo")
    mocker.patch("radioactive_ralph.pr_manager.get_forge_client", side_effect=Exception("API Error"))
    
    pr = pr_to_model(ForgePR(number=1, title="t", author="a", branch="b", head_sha="", is_draft=False, url="", updated_at=datetime.now(UTC)), "org/repo")
    
    result = await merge_pr(pr, Path("/repo"))
    assert result is False
