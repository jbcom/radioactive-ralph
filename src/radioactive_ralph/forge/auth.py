"""GitHub auth token discovery.

Token resolution order (matches gh CLI convention):
  1. GH_TOKEN env var
  2. GITHUB_TOKEN env var
  3. `gh auth token` subprocess (with full path resolved via shutil.which)
  4. Raise AuthError with a helpful message
"""

from __future__ import annotations

import logging
import os
import shutil
import subprocess

logger = logging.getLogger(__name__)

GITHUB_API_VERSION = "2022-11-28"


class AuthError(Exception):
    """Raised when no GitHub token can be found."""


def inside_claude_code() -> bool:
    """Return True if we are running inside a Claude Code subprocess."""
    return os.environ.get("CLAUDECODE") == "1"


def get_github_token() -> str:
    """Discover GitHub token via standard fallback chain."""
    for var in ("GH_TOKEN", "GITHUB_TOKEN"):
        if tok := os.environ.get(var):
            logger.debug("GitHub token from %s env var", var)
            return tok

    # Resolve the full path to `gh` via shutil.which to avoid command injection
    # via PATH manipulation. If the binary isn't on PATH, skip the subprocess.
    gh_path = shutil.which("gh")
    if gh_path:
        try:
            result = subprocess.run(
                [gh_path, "auth", "token"],
                capture_output=True,
                text=True,
                timeout=5,
            )
            if result.returncode == 0 and (tok := result.stdout.strip()):
                logger.debug("GitHub token from `gh auth token`")
                return tok
        except subprocess.TimeoutExpired:
            logger.debug("`gh auth token` timed out; falling through to AuthError")

    if inside_claude_code():
        hint = (
            "Running inside Claude Code — consider using the GitHub MCP server "
            "for interactive operations, or set GH_TOKEN / GITHUB_TOKEN."
        )
    else:
        hint = (
            "Set GH_TOKEN or GITHUB_TOKEN env var, "
            "or install and authenticate the gh CLI (`gh auth login`)."
        )

    raise AuthError(f"No GitHub token found. {hint}")
