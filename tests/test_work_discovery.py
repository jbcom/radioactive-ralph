"""Tests for work discovery logic."""

from __future__ import annotations

from pathlib import Path
import pytest

from radioactive_ralph.work_discovery import discover_work


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
