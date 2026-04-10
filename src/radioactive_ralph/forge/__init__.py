"""Forge abstraction — auto-detect and instantiate the right forge client.

Supports GitHub, GitLab, Gitea, and Forgejo by detecting the remote URL.
Use `get_forge_client(remote_url)` to get the right client for a repo.
"""

from __future__ import annotations

import re

from .base import ForgeCI, ForgeClient, ForgeInfo, ForgePR, PRCreateParams
from .gitea import GiteaForge
from .github import GitHubForge
from .gitlab import GitLabForge


def detect_forge(remote_url: str) -> ForgeInfo:
    """Parse a git remote URL and return forge metadata.

    Supports:
    - git@github.com:org/repo.git
    - https://github.com/org/repo.git
    - git@gitlab.com:org/repo.git
    - https://gitlab.com/org/repo.git
    - https://git.example.com/org/repo.git  (Gitea/Forgejo)

    Returns a ForgeInfo with host, slug, and forge_type.
    Raises ValueError if the URL cannot be parsed.
    """
    # Normalize SSH → HTTPS-like
    url = remote_url.strip()
    # git@host:path → https://host/path
    url = re.sub(r"^git@([^:]+):", r"https://\1/", url)
    url = url.rstrip("/").removesuffix(".git")

    match = re.match(r"https?://([^/]+)/(.+)", url)
    if not match:
        raise ValueError(f"Cannot parse remote URL: {remote_url!r}")

    host, slug = match.group(1), match.group(2)

    if host == "github.com":
        forge_type = "github"
    elif host == "gitlab.com" or host.startswith("gitlab."):
        forge_type = "gitlab"
    else:
        # Assume Gitea/Forgejo for any other self-hosted instance
        forge_type = "gitea"

    return ForgeInfo(host=host, slug=slug, forge_type=forge_type)


def get_forge_client(remote_url: str) -> ForgeClient:
    """Return the appropriate ForgeClient for a remote URL.

    Token discovery is handled by each forge implementation.
    Raises ValueError for unrecognised URLs, AuthError if no token found.
    """
    info = detect_forge(remote_url)
    if info.forge_type == "github":
        return GitHubForge(info)
    if info.forge_type == "gitlab":
        return GitLabForge(info)
    return GiteaForge(info)


__all__ = [
    "ForgeCI",
    "ForgeClient",
    "ForgeInfo",
    "ForgePR",
    "GitHubForge",
    "GitLabForge",
    "GiteaForge",
    "PRCreateParams",
    "detect_forge",
    "get_forge_client",
]
