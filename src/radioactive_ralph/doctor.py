"""Health checks for the orchestrator environment."""

from __future__ import annotations

import os
import shutil

from rich.console import Console
from rich.table import Table

from radioactive_ralph.config import RadioactiveRalphConfig

console = Console()


def check_health(config: RadioactiveRalphConfig) -> bool:
    """Run a series of health checks and print results.

    Args:
        config: Orchestrator configuration.

    Returns:
        True if all critical checks pass, False otherwise.
    """
    table = Table(title="Ralph Doctor - Health Report", border_style="bold magenta")
    table.add_column("Check", style="cyan")
    table.add_column("Status", style="bold")
    table.add_column("Details")

    all_pass = True

    # 1. Check for 'claude' CLI
    if shutil.which("claude"):
        table.add_row("Claude CLI", "[green]OK[/]", "Found in PATH")
    else:
        table.add_row("Claude CLI", "[red]MISSING[/]", "Install claude-code first")
        all_pass = False

    # 2. Check for 'gh' CLI
    if shutil.which("gh"):
        table.add_row("GitHub CLI", "[green]OK[/]", "Found in PATH")
    else:
        table.add_row("GitHub CLI", "[yellow]MISSING[/]", "gh CLI recommended for token discovery")

    # 3. Check for Anthropic API Key
    if os.environ.get("ANTHROPIC_API_KEY"):
        table.add_row("Anthropic Key", "[green]OK[/]", "Found in environment")
    else:
        table.add_row("Anthropic Key", "[red]MISSING[/]", "Set ANTHROPIC_API_KEY")
        all_pass = False

    console.print(table)
    return all_pass
