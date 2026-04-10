"""Local git operations via GitPython, exposed as async wrappers.

All git operations run in a thread executor via ``asyncio.to_thread`` because
GitPython is synchronous. This keeps the asyncio event loop unblocked.
"""

from __future__ import annotations

import asyncio
import logging
from collections.abc import Callable
from pathlib import Path
from typing import Any, TypeVar

import git

logger = logging.getLogger(__name__)

T = TypeVar("T")


def _open_repo(path: str | Path) -> git.Repo:
    """Open a git repository, raising a clear error if it's not a repo.

    Args:
        path: Directory containing or inside the git repo.

    Returns:
        A :class:`git.Repo` instance.

    Raises:
        ValueError: If the path is not a git repository.
    """
    try:
        return git.Repo(str(path), search_parent_directories=True)
    except (git.InvalidGitRepositoryError, git.NoSuchPathError) as e:
        raise ValueError(f"Not a git repository: {path}") from e


class GitClient:
    """Async-friendly wrapper around GitPython for a single repository."""

    def __init__(self, repo_path: str | Path) -> None:
        """Initialize the git client.

        Args:
            repo_path: Local path to the repository.
        """
        self._path = Path(repo_path)
        self._repo_cache: git.Repo | None = None
        self._lock = asyncio.Lock()

    async def _run(self, fn: Callable[..., T], *args: Any, **kwargs: Any) -> T:
        """Run a synchronous function in a thread, returning its result.

        Args:
            fn: The synchronous function to execute.
            *args: Positional arguments.
            **kwargs: Keyword arguments.

        Returns:
            The result of the function call.
        """
        return await asyncio.to_thread(fn, *args, **kwargs)

    async def _repo(self) -> git.Repo:
        """Open and cache the git repository instance.

        Returns:
            The cached :class:`git.Repo` instance.
        """
        async with self._lock:
            if self._repo_cache is None:
                self._repo_cache = await self._run(_open_repo, self._path)
            return self._repo_cache

    async def get_remote_url(self, remote: str = "origin") -> str | None:
        """Return the URL of a named remote, or None if not found.

        Args:
            remote: Name of the remote (default: "origin").

        Returns:
            The remote URL string, or None.
        """
        try:
            repo = await self._repo()
            return await self._run(lambda r: str(r.remotes[remote].url), repo)
        except (IndexError, AttributeError, ValueError, KeyError):
            return None

    async def current_branch(self) -> str | None:
        """Return the name of the current branch, or None if detached HEAD.

        Returns:
            Branch name string, or None.
        """
        try:
            repo = await self._repo()
            return await self._run(lambda r: str(r.active_branch.name), repo)
        except (TypeError, ValueError):
            return None

    async def pull(self, remote: str = "origin", ff_only: bool = True) -> bool:
        """Pull from a remote.

        Args:
            remote: Remote name to pull from.
            ff_only: If True, only allow fast-forward merges.

        Returns:
            True if pull succeeded, False on error.
        """
        def _pull(r: git.Repo) -> bool:
            try:
                rem = r.remote(remote)
                kwargs: dict[str, Any] = {"ff_only": ff_only} if ff_only else {}
                rem.pull(**kwargs)
                return True
            except (git.GitCommandError, ValueError) as e:
                logger.warning("git pull failed: %s", e)
                return False

        repo = await self._repo()
        return await self._run(_pull, repo)

    async def is_dirty(self) -> bool:
        """Return True if the working tree has uncommitted changes.

        Returns:
            Boolean dirty status.
        """
        repo = await self._repo()
        return await self._run(lambda r: bool(r.is_dirty()), repo)

    async def get_head_sha(self) -> str:
        """Return the SHA of the current HEAD commit.

        Returns:
            Full 40-character SHA hex string.
        """
        repo = await self._repo()
        return await self._run(lambda r: str(r.head.commit.hexsha), repo)

    async def list_remotes(self) -> dict[str, str]:
        """Return a mapping of remote name → URL for all remotes.

        Returns:
            Dict where keys are remote names and values are URLs.
        """
        def _remotes(r: git.Repo) -> dict[str, str]:
            return {str(rem.name): str(rem.url) for rem in r.remotes}

        repo = await self._repo()
        return await self._run(_remotes, repo)
