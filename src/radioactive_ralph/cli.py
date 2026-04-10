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
