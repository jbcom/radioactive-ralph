"""Forge abstraction — auto-detect and instantiate the right forge client.

Supports GitHub, GitLab, Gitea, and Forgejo by detecting the remote URL.
Use `get_forge_client(remote_url)` to get the right client for a repo.
"""

from __future__ import annotations

import os
import re

from radioactive_ralph.forge.base import ForgeCI, ForgeClient, ForgeInfo, ForgePR, PRCreateParams
from radioactive_ralph.forge.gitea import GiteaForge
from radioactive_ralph.forge.github import GitHubForge
from radioactive_ralph.forge.gitlab import GitLabForge


def detect_forge(remote_url: str) -> ForgeInfo:
    """Parse a git remote URL and return forge metadata.

    Supports:
    - git@github.com:org/repo.git
    - https://github.com/org/repo.git
    - git@gitlab.com:org/repo.git
    - https://gitlab.com/org/repo.git
    - https://git.example.com/org/repo.git  (Gitea/Forgejo)

    Args:
        remote_url: The git remote URL to parse.

    Returns:
        A ForgeInfo object containing host, slug, forge_type, and api_base_url.

    Raises:
        ValueError: If the URL cannot be parsed.
    """
    # Normalize SSH → HTTPS-like
    url = remote_url.strip()
    # git@host:path → https://host/path
    url = re.sub(r"^git@([^:]+):", r"https://\1/", url)
    url = url.rstrip("/").removesuffix(".git")

    match = re.match(r"(https?://)([^/]+)/(.+)", url)
    if not match:
        raise ValueError(f"Cannot parse remote URL: {remote_url!r}")

    scheme, host, slug = match.group(1), match.group(2), match.group(3)

    # Allow environment override for self-hosted instances
    # e.g. FORGE_TYPE_OVERRIDE=gitlab for git.example.com
    forge_type = os.environ.get("FORGE_TYPE_OVERRIDE")

    if not forge_type:
        if host == "github.com":
            forge_type = "github"
        elif host == "gitlab.com" or host.startswith("gitlab."):
            forge_type = "gitlab"
        else:
            # Assume Gitea/Forgejo for any other self-hosted instance
            forge_type = "gitea"

    # Determine api_base_url
    if forge_type == "github":
        if host == "github.com":
            api_base_url = "https://api.github.com"
        else:
            # GitHub Enterprise Server
            api_base_url = f"{scheme}{host}/api/v3"
    elif forge_type == "gitlab":
        api_base_url = f"{scheme}{host}/api/v4"
    else:
        # Gitea / Forgejo
        api_base_url = f"{scheme}{host}/api/v1"

    return ForgeInfo(host=host, slug=slug, forge_type=forge_type, api_base_url=api_base_url)


def get_forge_client(remote_url: str) -> ForgeClient:
    """Return the appropriate ForgeClient for a remote URL.

    Token discovery is handled by each forge implementation.

    Args:
        remote_url: The git remote URL to get a client for.

    Returns:
        An instantiated ForgeClient appropriate for the remote URL.

    Raises:
        ValueError: If the URL cannot be parsed.
        AuthError: If no token is found (raised by the client implementation).
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
