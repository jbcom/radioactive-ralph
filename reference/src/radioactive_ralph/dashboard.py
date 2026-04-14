"""Visual dashboard for the orchestrator using Rich."""

from __future__ import annotations

from rich.console import Console
from rich.table import Table

from radioactive_ralph.config import RadioactiveRalphConfig
from radioactive_ralph.models import OrchestratorState

console = Console()


def render_dashboard(state: OrchestratorState, config: RadioactiveRalphConfig) -> None:
    """Render a static snapshot of the orchestrator state to the console.

    Args:
        state: Current orchestrator state.
        config: Orchestrator configuration.
    """
    table = Table(title="Orchestrator Status", border_style="magenta")
    table.add_column("Stat", style="cyan")
    table.add_column("Value", style="green")

    table.add_row("Cycle Count", str(state.cycle_count))
    table.add_row("Active Agents", str(len(state.active_runs)))
    table.add_row("Queued Work", str(len(state.work_queue)))
    table.add_row("Last Scan", str(state.last_scan or "Never"))

    console.print(table)

    if state.work_queue:
        work_table = Table(title="Work Queue", border_style="blue")
        work_table.add_column("Repo")
        work_table.add_column("Priority")
        work_table.add_column("Description")

        for item in state.work_queue[:10]:
            work_table.add_row(item.repo_name, item.priority.name, item.description)
        console.print(work_table)
