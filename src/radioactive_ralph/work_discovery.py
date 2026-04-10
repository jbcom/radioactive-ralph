"""Work discovery — assess repos, read STATE.md, rank tasks."""

from __future__ import annotations

import hashlib
import logging
from pathlib import Path

from .models import WorkItem, WorkPriority

logger = logging.getLogger(__name__)

REQUIRED_FILES = [
    "CLAUDE.md",
    "AGENTS.md",
    "README.md",
    "CHANGELOG.md",
    "STANDARDS.md",
]

REQUIRED_DOCS = [
    "docs/ARCHITECTURE.md",
    "docs/DESIGN.md",
    "docs/TESTING.md",
    "docs/STATE.md",
]


def make_work_id(repo: str, source: str, description: str) -> str:
    """Generate a deterministic work item ID."""
    raw = f"{repo}:{source}:{description}"
    return hashlib.sha256(raw.encode()).hexdigest()[:12]


def discover_missing_files(repo_path: Path) -> list[WorkItem]:
    """Find required project files that are missing."""
    items: list[WorkItem] = []
    repo_name = repo_path.name

    for filename in REQUIRED_FILES:
        if not (repo_path / filename).exists():
            items.append(
                WorkItem(
                    id=make_work_id(repo_name, "missing_file", filename),
                    repo_path=str(repo_path),
                    description=f"Create missing required file: {filename}",
                    priority=WorkPriority.MISSING_FILES,
                    source="file_scan",
                    context=f"{filename} is required per project standards",
                )
            )

    for doc_path in REQUIRED_DOCS:
        if not (repo_path / doc_path).exists():
            items.append(
                WorkItem(
                    id=make_work_id(repo_name, "missing_doc", doc_path),
                    repo_path=str(repo_path),
                    description=f"Create missing documentation: {doc_path}",
                    priority=WorkPriority.MISSING_FILES,
                    source="doc_scan",
                    context=f"{doc_path} is required per project standards",
                )
            )

    return items


def parse_state_md(repo_path: Path) -> list[WorkItem]:
    """Parse docs/STATE.md for next items."""
    state_file = repo_path / "docs" / "STATE.md"
    if not state_file.exists():
        return []

    content = state_file.read_text(encoding="utf-8")
    items: list[WorkItem] = []
    repo_name = repo_path.name

    in_next_section = False
    for line in content.splitlines():
        stripped = line.strip()

        if stripped.lower().startswith("## next") or stripped.lower().startswith("## upcoming"):
            in_next_section = True
            continue
        elif stripped.startswith("## ") and in_next_section:
            in_next_section = False
            continue

        if in_next_section and stripped.startswith("- "):
            task_desc = stripped[2:].strip()
            if task_desc.startswith("[ ] "):
                task_desc = task_desc[4:]
            elif task_desc.startswith("[x] ") or task_desc.startswith("[X] "):
                continue

            if task_desc:
                items.append(
                    WorkItem(
                        id=make_work_id(repo_name, "state_md", task_desc),
                        repo_path=str(repo_path),
                        description=task_desc,
                        priority=WorkPriority.STATE_NEXT,
                        source="docs/STATE.md",
                    )
                )

    return items


def parse_design_md(repo_path: Path) -> list[WorkItem]:
    """Parse docs/DESIGN.md for feature ideas."""
    design_file = repo_path / "docs" / "DESIGN.md"
    if not design_file.exists():
        return []

    content = design_file.read_text(encoding="utf-8")
    items: list[WorkItem] = []
    repo_name = repo_path.name

    in_features = False
    for line in content.splitlines():
        stripped = line.strip()

        if "feature" in stripped.lower() and stripped.startswith("## "):
            in_features = True
            continue
        elif stripped.startswith("## ") and in_features:
            in_features = False
            continue

        if in_features and stripped.startswith("- "):
            desc = stripped[2:].strip()
            if desc.startswith("[ ] "):
                desc = desc[4:]
            elif desc.startswith("[x] ") or desc.startswith("[X] "):
                continue

            if desc:
                items.append(
                    WorkItem(
                        id=make_work_id(repo_name, "design_md", desc),
                        repo_path=str(repo_path),
                        description=desc,
                        priority=WorkPriority.DESIGN_FEATURE,
                        source="docs/DESIGN.md",
                    )
                )

    return items


def discover_work(repo_path: Path) -> list[WorkItem]:
    """Run all discovery sources for a single repo and return ranked items."""
    all_items: list[WorkItem] = []

    all_items.extend(discover_missing_files(repo_path))
    all_items.extend(parse_state_md(repo_path))
    all_items.extend(parse_design_md(repo_path))

    all_items.sort(key=lambda w: (w.priority.value, w.created_at))
    return all_items


def discover_all_repos(repo_paths: list[Path]) -> list[WorkItem]:
    """Discover work across all configured repos, ranked by priority."""
    all_items: list[WorkItem] = []

    for repo_path in repo_paths:
        if not repo_path.is_dir():
            logger.warning("Repo path does not exist: %s", repo_path)
            continue
        items = discover_work(repo_path)
        all_items.extend(items)

    all_items.sort(key=lambda w: (w.priority.value, w.created_at))
    return all_items
