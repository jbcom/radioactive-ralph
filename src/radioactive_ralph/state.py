"""Durable state persistence for radioactive-ralph orchestrator."""

from __future__ import annotations

from pathlib import Path

from radioactive_ralph.models import OrchestratorState, WorkItem


def default_state_path() -> Path:
    """Return the default state file location.

    Returns:
        The path to the default state file (~/.radioactive-ralph/state.json).
    """
    return Path.home() / ".radioactive-ralph" / "state.json"


def load_state(path: Path | None = None) -> OrchestratorState:
    """Load orchestrator state from disk. Returns empty state if file missing.

    Args:
        path: Optional path to the state file. Defaults to `default_state_path()`.

    Returns:
        The parsed OrchestratorState object.
    """
    if path is None:
        path = default_state_path()

    if not path.exists():
        return OrchestratorState()

    raw = path.read_text(encoding="utf-8")
    if not raw.strip():
        return OrchestratorState()

    return OrchestratorState.model_validate_json(raw)


def save_state(state: OrchestratorState, path: Path | None = None) -> Path:
    """Persist orchestrator state to disk. Creates parent dirs if needed.

    Args:
        state: The OrchestratorState object to save.
        path: Optional path to the state file. Defaults to `default_state_path()`.

    Returns:
        The path where the state was saved.
    """
    if path is None:
        path = default_state_path()

    path.parent.mkdir(parents=True, exist_ok=True)

    serialized = state.model_dump_json(indent=2)
    path.write_text(serialized + "\n", encoding="utf-8")
    return path


def reset_state(path: Path | None = None) -> OrchestratorState:
    """Reset state to empty and persist.

    Args:
        path: Optional path to the state file. Defaults to `default_state_path()`.

    Returns:
        The newly created, empty OrchestratorState object.
    """
    fresh = OrchestratorState()
    save_state(fresh, path)
    return fresh


def export_state_summary(state: OrchestratorState) -> dict[str, object]:
    """Export a human-readable summary of current state.

    Args:
        state: The current OrchestratorState object.

    Returns:
        A dictionary containing a summary of the state.
    """
    active_repos = {run.task.repo_name for run in state.active_runs}
    completed_repos = {run.task.repo_name for run in state.completed_runs}

    return {
        "active_agents": len(state.active_runs),
        "active_repos": sorted(active_repos),
        "completed_runs": len(state.completed_runs),
        "completed_repos": sorted(completed_repos),
        "merge_queue_size": len(state.merge_queue),
        "work_queue_size": len(state.work_queue),
        "cycle_count": state.cycle_count,
        "last_scan": state.last_scan.isoformat() if state.last_scan else None,
        "last_discovery": state.last_discovery.isoformat() if state.last_discovery else None,
    }


def prune_completed(state: OrchestratorState, keep: int = 100) -> int:
    """Prune old completed runs, keeping the most recent `keep` entries.

    Args:
        state: The current OrchestratorState object.
        keep: The number of recent completed runs to keep.

    Returns:
        The number of completed runs that were pruned.
    """
    before = len(state.completed_runs)
    if before <= keep:
        return 0

    state.completed_runs = sorted(
        state.completed_runs,
        key=lambda r: r.started_at,
        reverse=True,
    )[:keep]

    return before - len(state.completed_runs)


def merge_work_items(
    state: OrchestratorState, new_items: list[WorkItem]
) -> int:
    """Add work items to the queue, deduplicating by ID. Returns count added.

    Args:
        state: The current OrchestratorState object.
        new_items: A list of new WorkItem objects to add.

    Returns:
        The number of new work items successfully added to the queue.
    """
    from radioactive_ralph.models import WorkItem

    existing_ids = {item.id for item in state.work_queue}
    active_ids = {run.task.id for run in state.active_runs}
    skip_ids = existing_ids | active_ids

    added = 0
    for item in new_items:
        if not isinstance(item, WorkItem):
            continue
        if item.id not in skip_ids:
            state.work_queue.append(item)
            skip_ids.add(item.id)
            added += 1

    state.work_queue.sort(key=lambda w: (w.priority.value, w.created_at))
    return added
