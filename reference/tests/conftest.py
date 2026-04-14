"""Shared test fixtures."""

from __future__ import annotations

from pathlib import Path

import pytest


@pytest.fixture
def tmp_repo(tmp_path: Path) -> Path:
    """A tmp directory with a .git dir to simulate a repo.

    Args:
        tmp_path: Description of tmp_path.

    Returns:
        Description of return value.

    """
    (tmp_path / ".git").mkdir()
    (tmp_path / "README.md").write_text("# Test Repo\n")
    return tmp_path


@pytest.fixture
def tmp_repo_with_docs(tmp_repo: Path) -> Path:
    """A tmp repo with some standard docs already present.

    Args:
        tmp_repo: Description of tmp_repo.

    Returns:
        Description of return value.

    """
    (tmp_repo / "CLAUDE.md").write_text("# CLAUDE\n")
    (tmp_repo / "docs").mkdir()
    (tmp_repo / "docs" / "ARCHITECTURE.md").write_text("# Arch\n")
    return tmp_repo
