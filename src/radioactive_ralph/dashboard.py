"""Rich Live terminal dashboard for radioactive-ralph.

Reads state from the orchestrator's state file and the in-process
`recent_events` ring buffer, and renders a refreshing multi-panel view.
Read-only: does not drive the orchestrator, so it can run in a separate
terminal while the daemon runs elsewhere. Refreshes every 1s.
"""

from __future__ import annotations

import time
from datetime import UTC, datetime
from pathlib import Path

from rich.align import Align
from rich.console import Group
from rich.layout import Layout
from rich.live import Live
from rich.panel import Panel
from rich.table import Table
from rich.text import Text

from .models import OrchestratorState, PRStatus
from .ralph_says import _COLORS, Variant, recent_events
from .state import load_state

_PR_STATUS_STYLE: dict[PRStatus, str] = {
    PRStatus.MERGE_READY: "bright_green",
    PRStatus.NEEDS_REVIEW: "yellow",
    PRStatus.NEEDS_FIXES: "orange3",
    PRStatus.CI_FAILING: "bright_red",
    PRStatus.IN_PROGRESS: "cyan",
    PRStatus.STALE: "grey62",
    PRStatus.DRAFT: "grey50",
}


def _fmt_duration(seconds: float) -> str:
    """Format a duration in seconds as a compact string."""
    if seconds < 60:
        return f"{int(seconds)}s"
    if seconds < 3600:
        return f"{int(seconds // 60)}m{int(seconds % 60):02d}s"
    hours = int(seconds // 3600)
    minutes = int((seconds % 3600) // 60)
    return f"{hours}h{minutes:02d}m"


def _infer_daemon_state(state: OrchestratorState) -> str:
    """Guess the current daemon phase from state shape."""
    if state.active_runs:
        return "executing"
    if state.merge_queue:
        return "merging"
    if state.work_queue:
        return "discovered"
    return "idle"


def _header(state: OrchestratorState, variant: Variant, started: datetime) -> Panel:
    """Top banner with variant, cycle, uptime, and daemon state."""
    primary = _COLORS[variant]["primary"]
    accent = _COLORS[variant]["accent"]
    uptime = _fmt_duration((datetime.now(UTC) - started).total_seconds())
    phase = _infer_daemon_state(state)

    text = Text()
    text.append("◉ ", style=primary)
    text.append(variant.value, style=f"bold {primary}")
    text.append("   ")
    text.append("cycle ", style="dim")
    text.append(str(state.cycle_count), style=f"bold {accent}")
    text.append("   ")
    text.append("uptime ", style="dim")
    text.append(uptime, style=f"bold {accent}")
    text.append("   ")
    text.append("state ", style="dim")
    text.append(phase, style=f"bold {accent}")

    return Panel(
        Align.center(text, vertical="middle"),
        border_style=primary,
        title=f"[{primary}]radioactive-ralph · live dashboard[/{primary}]",
        padding=(0, 1),
    )


def _pr_table(state: OrchestratorState, variant: Variant) -> Panel:
    """Most recent scanned PRs with status-colored classification."""
    primary = _COLORS[variant]["primary"]
    t = Table(expand=True, show_edge=False, pad_edge=False, box=None)
    t.add_column("Repo", style="bright_white", no_wrap=True)
    t.add_column("PR", justify="right", width=6)
    t.add_column("Status", width=14)
    t.add_column("CI", justify="center", width=4)
    t.add_column("Title", overflow="ellipsis")

    prs = sorted(state.merge_queue, key=lambda p: p.updated_at, reverse=True)[:12]
    if not prs:
        t.add_row("[dim]—[/dim]", "[dim]—[/dim]", "[dim]no PRs[/dim]", "", "")
    else:
        for pr in prs:
            style = _PR_STATUS_STYLE.get(pr.status, "white")
            ci = "[green]✓[/green]" if pr.ci_passed else "[red]✗[/red]"
            t.add_row(
                pr.repo.split("/")[-1],
                f"#{pr.number}",
                f"[{style}]{pr.status.value}[/{style}]",
                ci,
                pr.title[:60],
            )

    return Panel(t, title="[bold]Pull requests[/bold]", border_style=primary)


def _work_table(state: OrchestratorState, variant: Variant) -> Panel:
    """Top 10 items in the work queue, priority-ordered."""
    primary = _COLORS[variant]["primary"]
    t = Table(expand=True, show_edge=False, pad_edge=False, box=None)
    t.add_column("Pri", width=4, justify="right")
    t.add_column("Repo", style="bright_white", no_wrap=True)
    t.add_column("Description", overflow="ellipsis")

    items = state.work_queue[:10]
    if not items:
        t.add_row("[dim]—[/dim]", "[dim]—[/dim]", "[dim]queue empty[/dim]")
    else:
        for item in items:
            t.add_row(
                f"[bold]P{item.priority.value}[/bold]",
                item.repo_name,
                item.description[:70],
            )

    return Panel(
        t,
        title=f"[bold]Work queue[/bold] [dim]({len(state.work_queue)} total)[/dim]",
        border_style=primary,
    )


def _agents_panel(state: OrchestratorState, variant: Variant) -> Panel:
    """Currently-running agents with task, repo, and elapsed time."""
    primary = _COLORS[variant]["primary"]
    t = Table(expand=True, show_edge=False, pad_edge=False, box=None)
    t.add_column("Repo", style="bright_white", no_wrap=True)
    t.add_column("Task", overflow="ellipsis")
    t.add_column("Elapsed", width=8, justify="right")

    active = state.active_runs
    if not active:
        t.add_row("[dim]—[/dim]", "[dim]no active agents[/dim]", "")
    else:
        now = datetime.now(UTC)
        for run in active:
            elapsed = (now - run.started_at).total_seconds()
            t.add_row(
                run.task.repo_name,
                run.task.description[:60],
                _fmt_duration(elapsed),
            )

    return Panel(
        t,
        title=f"[bold]Active agents[/bold] [dim]({len(active)})[/dim]",
        border_style=primary,
    )


def _activity_panel(variant: Variant) -> Panel:
    """Last 20 Ralph events from the in-process ring buffer."""
    primary = _COLORS[variant]["primary"]
    events = recent_events()[-20:]

    if not events:
        body: Group | Text = Text(
            "Ralph hasn't said anything yet.", style="dim italic"
        )
    else:
        lines: list[Text] = []
        for _ev_variant, markup, ts in events:
            stamp = ts.astimezone().strftime("%H:%M:%S")
            line = Text()
            line.append(f"{stamp}  ", style="dim")
            line.append_text(Text.from_markup(markup))
            lines.append(line)
        body = Group(*lines)

    return Panel(body, title="[bold]Recent activity[/bold]", border_style=primary)


def _footer(state: OrchestratorState, variant: Variant) -> Panel:
    """Bottom stats strip: merged / agents / cost / failures."""
    primary = _COLORS[variant]["primary"]
    accent = _COLORS[variant]["accent"]

    succeeded = sum(
        1 for r in state.completed_runs if r.result and r.result.succeeded
    )
    failed = len(state.completed_runs) - succeeded
    merged = sum(1 for r in state.completed_runs if r.result and r.result.pr_url)

    # Rough cost estimate: ~$0.05/agent average (bulk+default mix).
    cost = len(state.completed_runs) * 0.05

    text = Text()
    text.append("merged ", style="dim")
    text.append(f"{merged}", style=f"bold {accent}")
    text.append("   agents ", style="dim")
    text.append(f"{succeeded}", style="bold green")
    text.append("/", style="dim")
    text.append(f"{len(state.completed_runs)}", style=f"bold {accent}")
    text.append("   cost ", style="dim")
    text.append(f"~${cost:.2f}", style=f"bold {accent}")
    text.append("   failures ", style="dim")
    text.append(f"{failed}", style="bold red" if failed else "dim")

    return Panel(
        Align.center(text, vertical="middle"),
        border_style=primary,
        padding=(0, 1),
    )


def _sleeping_placeholder(variant: Variant) -> Panel:
    """Shown when the state file doesn't exist yet."""
    primary = _COLORS[variant]["primary"]
    body = Text.from_markup(
        "[bold]Ralph is sleeping (no state file yet).[/bold]\n\n"
        "[dim]Start the daemon with[/dim] [bright_white]ralph run[/bright_white]"
        " [dim]in another terminal.[/dim]"
    )
    return Panel(
        Align.center(body, vertical="middle"),
        title=f"[{primary}]{variant.value}[/{primary}]",
        border_style=primary,
        padding=(2, 4),
    )


def build_layout(
    state: OrchestratorState | None,
    variant: Variant = Variant.GREEN,
    started: datetime | None = None,
) -> Layout:
    """Assemble the full dashboard layout.

    Public, pure, side-effect-free entry point for tests and snapshot
    tooling. Safe to call repeatedly; returns a fresh ``rich.layout.Layout``
    every time. ``run_dashboard`` delegates to this function.
    """
    if started is None:
        started = datetime.now(UTC)
    return _build_layout(state, variant, started)


def _build_layout(
    state: OrchestratorState | None, variant: Variant, started: datetime
) -> Layout:
    """Assemble the full dashboard layout (internal)."""
    layout = Layout()

    if state is None:
        layout.update(_sleeping_placeholder(variant))
        return layout

    layout.split_column(
        Layout(name="header", size=3),
        Layout(name="body", ratio=1),
        Layout(name="footer", size=3),
    )
    layout["body"].split_row(
        Layout(name="left", ratio=1),
        Layout(name="right", ratio=1),
    )
    layout["left"].split_column(
        Layout(_pr_table(state, variant), ratio=1),
        Layout(_agents_panel(state, variant), ratio=1),
    )
    layout["right"].split_column(
        Layout(_work_table(state, variant), ratio=1),
        Layout(_activity_panel(variant), ratio=1),
    )
    layout["header"].update(_header(state, variant, started))
    layout["footer"].update(_footer(state, variant))
    return layout


def run_dashboard(
    state_path: Path,
    variant: Variant = Variant.GREEN,
    refresh_per_second: float = 1.0,
) -> None:
    """Launch the Live dashboard. Blocks until Ctrl+C."""
    started = datetime.now(UTC)

    def snapshot() -> OrchestratorState | None:
        try:
            return load_state(state_path)
        except FileNotFoundError:
            return None
        except Exception:
            return None

    with Live(
        _build_layout(snapshot(), variant, started),
        refresh_per_second=refresh_per_second,
        screen=True,
        transient=False,
    ) as live:
        try:
            while True:
                time.sleep(1.0 / max(refresh_per_second, 0.1))
                live.update(_build_layout(snapshot(), variant, started))
        except KeyboardInterrupt:
            pass
