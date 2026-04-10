"""Rich-powered logging setup for radioactive-ralph."""

from __future__ import annotations

import logging

from rich.logging import RichHandler
from rich.traceback import install as install_rich_tracebacks


def setup_logging(level: int = logging.INFO) -> None:
    """Configure rich logging with pretty tracebacks."""
    install_rich_tracebacks(show_locals=True, max_frames=5)
    logging.basicConfig(
        level=level,
        format="%(message)s",
        datefmt="[%X]",
        handlers=[
            RichHandler(
                rich_tracebacks=True,
                tracebacks_show_locals=True,
                show_path=True,
                markup=True,
            )
        ],
    )
    # Quiet noisy libraries
    logging.getLogger("httpx").setLevel(logging.WARNING)
    logging.getLogger("httpcore").setLevel(logging.WARNING)
    logging.getLogger("anthropic").setLevel(logging.WARNING)
