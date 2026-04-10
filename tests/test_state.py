"""Tests for state persistence and management."""

from __future__ import annotations

from pathlib import Path
from datetime import UTC, datetime

from radioactive_ralph.models import (
    AgentRun,
    OrchestratorState,
    WorkItem,
    WorkPriority,
)
from radioactive_ralph.state import load_state, merge_work_items, prune_completed, save_state


def test_load_fresh_state(tmp_path: Path) -> None:
    """Test that loading a non-existent file returns a fresh state."""
    state = load_state(tmp_path / "missing.json")
    assert isinstance(state, OrchestratorState)
    assert state.cycle_count == 0


def test_save_and_load_state(tmp_path: Path) -> None:
    """Test round-trip persistence of orchestrator state."""
    state_file = tmp_path / "state.json"
    state = OrchestratorState(cycle_count=42)
    save_state(state, state_file)
    
    loaded = load_state(state_file)
    assert loaded.cycle_count == 42


def test_merge_work_items() -> None:
    """Test that merging work items avoids duplicates by ID."""
    existing = [
        WorkItem(id="1", repo_path="p1", description="d1", priority=WorkPriority.LOW)
    ]
    new_items = [
        WorkItem(id="1", repo_path="p1", description="d1", priority=WorkPriority.LOW),
        WorkItem(id="2", repo_path="p2", description="d2", priority=WorkPriority.HIGH)
    ]
    
    merged = merge_work_items(existing, new_items)
    assert len(merged) == 2
    assert {item.id for item in merged} == {"1", "2"}


def test_prune_completed() -> None:
    """Test that completed runs are removed from active list."""
    item = WorkItem(id="w1", repo_path="p1", description="d1", priority=WorkPriority.LOW)
    runs = [
        AgentRun(item=item, process_id=1, started_at=datetime.now(UTC)),
        AgentRun(item=item, process_id=2, started_at=datetime.now(UTC), completed_at=datetime.now(UTC))
    ]
    
    active = prune_completed(runs)
    assert len(active) == 1
    assert active[0].process_id == 1
