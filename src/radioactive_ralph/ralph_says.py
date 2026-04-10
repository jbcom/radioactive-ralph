"UI and personality for the orchestrator."

from __future__ import annotations

import contextlib
import logging
from collections.abc import Generator
from datetime import UTC, datetime
from enum import Enum
from typing import Any

from rich.console import Console
from rich.panel import Panel

logger = logging.getLogger(__name__)
console = Console()


class Variant(Enum):
    """Available personality variants for Ralph."""

    SAVAGE = "savage"
    JOE_FIXIT = "joe-fixit"
    OLD_MAN = "old-man"


_MESSAGES = {
    Variant.SAVAGE: {
        "startup": "I'm awake. Time to break things.",
        "shutdown": "Going back to sleep. Don't touch my stuff.",
        "reviewing": "Looking at PR #{pr} in {repo}. Probably garbage.",
        "reviewed_approved": "PR #{pr} looks okay. I guess.",
        "reviewed_changes": "Found {count} issues in PR #{pr}. Try again.",
        "merging": "Squashing PR #{pr}. It's for the best.",
        "merged": "Merged PR #{pr}. One less thing to worry about.",
        "merge_failed": "Couldn't merge PR #{pr}. Figures.",
    },
    Variant.JOE_FIXIT: {
        "startup": "Fixit's here. What needs doing?",
        "shutdown": "Shift's over. Keep it clean.",
        "reviewing": "Taking a peek at PR #{pr} in {repo}.",
        "reviewed_approved": "Clean bill of health for PR #{pr}.",
        "reviewed_changes": "Found {count} spots for improvement in PR #{pr}.",
        "merging": "Putting PR #{pr} into main.",
        "merged": "PR #{pr} is in. Good work.",
        "merge_failed": "Hit a snag merging PR #{pr}. I'll look into it.",
    },
}

_RECENT_EVENTS: list[tuple[Variant, str, datetime]] = []


def ralph_says(variant: Variant, key: str, **kwargs: Any) -> None:
    """Print a themed message from Ralph.

    Args:
        variant: The personality variant to use.
        key: The message key to look up.
        **kwargs: Format parameters for the message.
    """
    templates = _MESSAGES.get(variant, _MESSAGES[Variant.SAVAGE])
    template = templates.get(key, f"Unknown message: {key}")
    rendered = template.format(**kwargs)

    _RECENT_EVENTS.append((variant, rendered, datetime.now(UTC)))
    console.print(f"[bold magenta]Ralph ({variant.value}):[/] {rendered}")


@contextlib.contextmanager
def ralph_panel(variant: Variant, title: str) -> Generator[None, None, None]:
    """Context manager that prints a rich panel.

    Args:
        variant: The personality variant (unused for now, for consistency).
        title: The panel title.
    """
    console.print(
        Panel(
            f"Starting {title}...",
            title=f"Radioactive Ralph - {variant.value}",
            border_style="magenta"
        )
    )
    yield
    console.print(f"Finished {title}.\n")
