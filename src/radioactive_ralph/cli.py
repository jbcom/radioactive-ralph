"""Command-line interface for the orchestrator.

CLI surface during the architectural rewrite
(docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md):

  ralph status   — show current orchestrator state             [implemented]
  ralph doctor   — check environment health                    [implemented]
  ralph run      — launch the daemon                           [stub — M2]

`init`, `attach`, `stop` land in M2 along with the real daemon.
"""

from __future__ import annotations

import logging
from pathlib import Path

import click
from rich.console import Console
from rich.logging import RichHandler

from radioactive_ralph.config import load_config
from radioactive_ralph.dashboard import render_dashboard
from radioactive_ralph.doctor import check_health

console = Console()

_REWRITE_MSG = (
    "ralph run is under rewrite — see "
    "docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md (M2). "
    "The replacement is a supervisor process launched via multiplexer "
    "(tmux/screen/setsid), exposing a Unix socket and managing "
    "`claude -p --input-format stream-json` subprocesses."
)


@click.group()
@click.option("--verbose", "-v", is_flag=True, help="Enable debug logging")
def main(verbose: bool) -> None:
    """Radioactive Ralph: autonomous development orchestrator."""
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(message)s",
        datefmt="[%X]",
        handlers=[RichHandler(rich_tracebacks=True)],
    )


@main.command()
@click.option(
    "--config",
    type=click.Path(exists=True, path_type=Path),
    help="Path to config TOML",
)
@click.option(
    "--variant",
    type=click.Choice(["savage", "joe-fixit", "old-man"]),
    default="savage",
)
def run(config: Path | None, variant: str) -> None:
    """Start the continuous orchestration loop (stub — M2)."""
    click.echo(_REWRITE_MSG, err=True)
    raise click.exceptions.Exit(2)


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
