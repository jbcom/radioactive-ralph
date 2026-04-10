"""Click CLI entry point for radioactive-ralph."""

from __future__ import annotations

import asyncio
import signal
from pathlib import Path

import click
from rich.console import Console
from rich.table import Table

console = Console()


@click.group()
def cli() -> None:
    """radioactive-ralph — autonomous continuous development orchestrator."""


@cli.command()
@click.option("--config", "-c", default="~/.radioactive-ralph/config.toml", help="Config file path")
@click.option("--debug", is_flag=True, help="Enable debug logging")
def run(config: str, debug: bool) -> None:
    """Start the orchestrator daemon."""
    import logging

    from .config import load_config
    from .logging_setup import setup_logging
    from .orchestrator import Orchestrator

    setup_logging(logging.DEBUG if debug else logging.INFO)
    cfg = load_config(Path(config).expanduser())
    orch = Orchestrator(cfg)
    console.print("[bold green]radioactive-ralph starting...[/bold green]")
    asyncio.run(orch.run())


@cli.command()
@click.option("--config", "-c", default="~/.radioactive-ralph/config.toml", help="Config file path")
def status(config: str) -> None:
    """Show current orchestrator state."""
    from .config import load_config
    from .state import load_state

    cfg = load_config(Path(config).expanduser())
    state = load_state(cfg.resolve_state_path())

    console.print(f"[bold]Cycle:[/bold] {state.cycle_count}")
    console.print(f"[bold]Active runs:[/bold] {len(state.active_runs)}")
    console.print(f"[bold]Work queue:[/bold] {len(state.work_queue)} items")
    console.print(f"[bold]Merge queue:[/bold] {len(state.merge_queue)} PRs")
    console.print(f"[bold]Completed:[/bold] {len(state.completed_runs)} runs")

    if state.work_queue:
        t = Table("Priority", "Repo", "Description")
        for item in state.work_queue[:10]:
            t.add_row(str(item.priority.name), item.repo_name, item.description[:60])
        console.print(t)


@cli.command()
@click.option("--config", "-c", default="~/.radioactive-ralph/config.toml", help="Config file path")
def discover(config: str) -> None:
    """Run work discovery across all repos and print findings."""
    from .config import load_config
    from .work_discovery import discover_all_repos

    cfg = load_config(Path(config).expanduser())
    items = discover_all_repos(cfg.all_repo_paths())

    t = Table("Priority", "Repo", "Description", "Source")
    for item in items:
        t.add_row(str(item.priority.name), item.repo_name, item.description[:60], item.source)
    console.print(t)
    console.print(f"[bold]{len(items)} work items discovered[/bold]")


@cli.group()
def pr() -> None:
    """PR management commands."""


@pr.command("list")
@click.option("--config", "-c", default="~/.radioactive-ralph/config.toml", help="Config file path")
def pr_list(config: str) -> None:
    """List all open PRs across all repos with classification."""
    from .config import load_config
    from .pr_manager import scan_all_repos

    cfg = load_config(Path(config).expanduser())
    pr_map = asyncio.run(scan_all_repos(cfg.all_repo_paths()))
    all_prs = [pr for prs in pr_map.values() for pr in prs]

    t = Table("Repo", "PR", "Status", "CI", "Title")
    for info in all_prs:
        ci = "[green]PASS[/green]" if info.ci_passed else "[red]FAIL[/red]"
        t.add_row(info.repo, f"#{info.number}", info.status.value, ci, info.title[:50])
    console.print(t)


@pr.command("merge")
@click.option("--config", "-c", default="~/.radioactive-ralph/config.toml", help="Config file path")
@click.option("--dry-run", is_flag=True, help="Print what would be merged without merging")
def pr_merge(config: str, dry_run: bool) -> None:
    """Merge all MERGE_READY PRs."""
    from .config import load_config
    from .pr_manager import merge_pr, scan_all_repos

    cfg = load_config(Path(config).expanduser())
    repo_paths = cfg.all_repo_paths()
    pr_map = asyncio.run(scan_all_repos(repo_paths))
    # Map repo name → local path for merge
    path_by_name = {p.name: p for p in repo_paths}
    ready = [
        (p, path_by_name.get(p.repo.split("/")[-1]))
        for prs in pr_map.values()
        for p in prs
        if p.is_mergeable
    ]

    if not ready:
        console.print("No PRs ready to merge.")
        return

    for info, local_path in ready:
        action = "[dry-run] Would merge" if dry_run else "Merging"
        console.print(f"{action}: {info.repo} #{info.number}")
        if not dry_run and local_path:
            asyncio.run(merge_pr(info, local_path))


@cli.command()
@click.option("--config", "-c", default="~/.radioactive-ralph/config.toml", help="Config file path")
@click.option(
    "--variant",
    "-v",
    default="green-ralph",
    help="Ralph variant to theme the dashboard with (e.g. green-ralph, old-man-ralph).",
)
@click.option(
    "--refresh",
    default=1.0,
    type=float,
    help="Refresh interval in Hz (default 1.0 = once per second).",
)
def dashboard(config: str, variant: str, refresh: float) -> None:
    """Open the Rich Live terminal dashboard (reads state, does not run the daemon)."""
    from .config import load_config
    from .dashboard import run_dashboard
    from .ralph_says import Variant

    cfg = load_config(Path(config).expanduser())
    try:
        v = Variant(variant)
    except ValueError:
        console.print(f"[red]Unknown variant: {variant}[/red]")
        known = ", ".join(m.value for m in Variant)
        console.print(f"[dim]Known variants: {known}[/dim]")
        raise SystemExit(2) from None

    run_dashboard(cfg.resolve_state_path(), variant=v, refresh_per_second=refresh)


@cli.command()
@click.option("--config", "-c", default=None, help="Config file path")
@click.option("--json", "as_json", is_flag=True, help="Emit JSON instead of a Rich table")
def doctor(config: str | None, as_json: bool) -> None:
    """Run diagnostic health checks on the ralph environment."""
    import json as json_mod
    import os as _os

    from .doctor import FAIL, OK, WARN, run_all_checks
    from .ralph_says import Variant, ralph_says

    cfg_path_str = (
        config
        or _os.environ.get("RALPH_CONFIG_PATH")
        or "~/.radioactive-ralph/config.toml"
    )
    cfg_path = Path(cfg_path_str).expanduser()
    report = run_all_checks(cfg_path)

    if as_json:
        console.print_json(json_mod.dumps(report.to_dict()))
        raise SystemExit(0 if report.ok else 1)

    ralph_says(Variant.PROFESSOR, "startup")
    t = Table("Check", "Status", "Detail", "Fix", show_lines=False)
    status_style = {
        OK: "[green]\u2713 OK[/green]",
        WARN: "[yellow]\u26a0 WARN[/yellow]",
        FAIL: "[red]\u2717 FAIL[/red]",
    }
    for r in report.results:
        t.add_row(r.name, status_style.get(r.status, r.status), r.detail, r.fix or "")
    console.print(t)
    console.print(
        f"[bold]{len(report.results)} checks, "
        f"[red]{report.failed} failed[/red], "
        f"[yellow]{report.warnings} warnings[/yellow].[/bold]"
    )
    if report.ok:
        ralph_says(Variant.PROFESSOR, "success")
    else:
        ralph_says(Variant.PROFESSOR, "error")
    raise SystemExit(0 if report.ok else 1)


@cli.command()
def stop() -> None:
    """Send SIGTERM to a running ralph daemon."""
    import subprocess

    result = subprocess.run(["pgrep", "-f", "ralph run"], capture_output=True, text=True)
    pids = result.stdout.strip().split()
    if not pids:
        console.print("No running ralph daemon found.")
        return
    for pid in pids:
        import os
        os.kill(int(pid), signal.SIGTERM)
        console.print(f"Sent SIGTERM to PID {pid}")


def main() -> None:
    cli()
