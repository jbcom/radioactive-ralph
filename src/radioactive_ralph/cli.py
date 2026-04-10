"""Command-line interface for the orchestrator."""

from __future__ import annotations

import asyncio
import logging
from pathlib import Path

import click
from rich.console import Console
from rich.logging import RichHandler

from radioactive_ralph.config import load_config
from radioactive_ralph.dashboard import render_dashboard
from radioactive_ralph.doctor import check_health
from radioactive_ralph.orchestrator import Orchestrator
from radioactive_ralph.ralph_says import Variant

console = Console()


@click.group()
@click.option("--verbose", "-v", is_flag=True, help="Enable debug logging")
def main(verbose: bool) -> None:
    """Radioactive Ralph: Autonomous development orchestrator."""
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(message)s",
        datefmt="[%X]",
        handlers=[RichHandler(rich_tracebacks=True)],
    )


@main.command()
@click.option("--config", type=click.Path(exists=True, path_type=Path), help="Path to config TOML")
@click.option("--variant", type=click.Choice(["savage", "joe-fixit", "old-man"]), default="savage")
def run(config: Path | None, variant: str) -> None:
    """Start the continuous orchestration loop."""
    cfg = load_config(config)
    orchestrator = Orchestrator(config=cfg, variant=Variant(variant))

    try:
        asyncio.run(orchestrator.run())
    except KeyboardInterrupt:
        orchestrator.stop()


@main.command()
def status() -> None:
    """Show the current status of the orchestrator and all repos."""
    cfg = load_config()
    from radioactive_ralph.state import load_state
    state = load_state(cfg.resolve_state_path())
    render_dashboard(state, cfg)


@main.command()
def doctor() -> None:
    """Check for configuration issues and environment health."""
    cfg = load_config()
    check_health(cfg)


if __name__ == "__main__":
    main()
