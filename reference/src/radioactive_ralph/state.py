"""Persistence and management of the orchestrator's state."""

from __future__ import annotations

import logging
from pathlib import Path

from radioactive_ralph.models import AgentRun, OrchestratorState, WorkItem

logger = logging.getLogger(__name__)


def load_state(path: Path) -> OrchestratorState:
    """Load the orchestrator state from a JSON file.

    Args:
        path: Path to the state file.

    Returns:
        The loaded OrchestratorState object, or a default one if file is missing.
    """
    if not path.is_file():
        return OrchestratorState()

    try:
        return OrchestratorState.model_validate_json(path.read_text(encoding="utf-8"))
    except Exception as e:
        logger.warning("Failed to load state from %s: %s. Starting fresh.", path, e)
        return OrchestratorState()


def save_state(state: OrchestratorState, path: Path) -> None:
    """Save the orchestrator state to a JSON file.

    Args:
        state: The OrchestratorState object to persist.
        path: Path to the state file.
    """
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(state.model_dump_json(indent=2), encoding="utf-8")
    except Exception as e:
        logger.error("Failed to save state to %s: %s", path, e)


def merge_work_items(existing: list[WorkItem], new_items: list[WorkItem]) -> list[WorkItem]:
    """Merge newly discovered work items into the existing queue, avoiding duplicates.

    Args:
        existing: The current work queue.
        new_items: Newly discovered work items.

    Returns:
        The merged list of unique work items.
    """
    seen_ids = {item.id for item in existing}
    merged = list(existing)

    for item in new_items:
        if item.id not in seen_ids:
            merged.append(item)
            seen_ids.add(item.id)

    return merged


def prune_completed(active_runs: list[AgentRun]) -> list[AgentRun]:
    """Remove completed runs from the active tracking list.

    Args:
        active_runs: List of currently tracked agent runs.

    Returns:
        The list of still active agent runs.
    """
    return [run for run in active_runs if run.is_active]
