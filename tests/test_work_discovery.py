"""Tests for work discovery logic."""

from __future__ import annotations

from pathlib import Path
import pytest

from radioactive_ralph.work_discovery import discover_work, discover_all_repos


@pytest.mark.asyncio
async def test_discover_all_repos_success(tmp_path: Path) -> None:
    """Test successful discovery across multiple repos."""
    repo1 = tmp_path / "repo1"
    repo2 = tmp_path / "repo2"
    repo1.mkdir()
    repo2.mkdir()
    
    # Both are completely empty, so they should generate both work items (missing tests, missing docs)
    items = await discover_all_repos([repo1, repo2])
    
    assert len(items) == 4 # 2 for repo1, 2 for repo2
    repos = {i.repo_path for i in items}
    assert str(repo1) in repos
    assert str(repo2) in repos


@pytest.mark.asyncio
async def test_discover_all_repos_exception(tmp_path: Path, mocker) -> None:
    """Test that exception in one repo doesn't stop others."""
    repo1 = tmp_path / "repo1"
    repo2 = tmp_path / "repo2"
    repo1.mkdir()
    repo2.mkdir()
    
    mock_discover = mocker.patch("radioactive_ralph.work_discovery.discover_work", side_effect=[
        Exception("Failed to scan repo1"),
        [mocker.Mock(description="Work from repo2")]
    ])
    mock_logger = mocker.patch("radioactive_ralph.work_discovery.logger")
    
    items = await discover_all_repos([repo1, repo2])
    
    assert len(items) == 1
    assert items[0].description == "Work from repo2"
    mock_logger.error.assert_called_once()


@pytest.mark.asyncio
async def test_discover_work_missing_architecture(tmp_path: Path) -> None:
    """Test that missing ARCHITECTURE.md is discovered."""
    # Scaffold a fake repo
    (tmp_path / "docs").mkdir()
    (tmp_path / "tests").mkdir()
    
    items = await discover_work(tmp_path)
    assert any("ARCHITECTURE.md" in i.description for i in items)


@pytest.mark.asyncio
async def test_discover_work_missing_tests(tmp_path: Path) -> None:
    """Test that missing tests/ directory is discovered."""
    # Scaffold a fake repo
    (tmp_path / "docs").mkdir()
    (tmp_path / "docs" / "ARCHITECTURE.md").touch()
    
    items = await discover_work(tmp_path)
    assert any("tests/" in i.description for i in items)
