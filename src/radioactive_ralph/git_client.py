"""Local git operations via GitPython, exposed as async wrappers.

All git operations run in a thread executor via ``asyncio.to_thread`` because
GitPython is synchronous. This keeps the asyncio event loop unblocked.

Typical usage::

    client = GitClient("/path/to/repo")
    remote_url = await client.get_remote_url()
    branch = await client.current_branch()
    await client.pull()

The module never shells out to the ``git`` binary — all operations go through
:mod:`git` (GitPython).
"""

from __future__ import annotations

import asyncio
import logging
from pathlib import Path

import git

logger = logging.getLogger(__name__)


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
    except git.InvalidGitRepositoryError as e:
        raise ValueError(f"Not a git repository: {path}") from e


_WRITE_LOCKS: dict[str, asyncio.Lock] = {}


def _get_write_lock(path: Path) -> asyncio.Lock:
    """Return (creating if needed) a per-repo write lock.

    Used to serialise mutating git operations (pull, checkout) so that
    concurrent forge scans on the same repo cannot race.
    """
    key = str(path.resolve())
    if key not in _WRITE_LOCKS:
        _WRITE_LOCKS[key] = asyncio.Lock()
    return _WRITE_LOCKS[key]


class GitClient:
    """Async-friendly wrapper around GitPython for a single repository.

    All blocking GitPython operations are wrapped in :func:`asyncio.to_thread`
    so they do not block the event loop.

    Mutating operations (``pull``) acquire a per-path :class:`asyncio.Lock`
    to prevent concurrent writes to the same working tree when multiple
    coroutines operate on the same repo simultaneously.

    Args:
        repo_path: Path to the git repository root (or any subdirectory).

    Example::

        client = GitClient("/srv/repos/my-project")
        url = await client.get_remote_url()
    """

    def __init__(self, repo_path: str | Path) -> None:
        self._path = Path(repo_path)
        self._write_lock = _get_write_lock(self._path)

    async def _run(self, fn, *args, **kwargs):  # type: ignore[no-untyped-def]
        """Run a synchronous function in a thread, returning its result."""
        return await asyncio.to_thread(fn, *args, **kwargs)

    async def _repo(self) -> git.Repo:
        """Open the repository (thread-safe, cached per call)."""
        return await self._run(_open_repo, self._path)

    async def get_remote_url(self, remote: str = "origin") -> str | None:
        """Return the URL of a named remote, or None if not found.

        Args:
            remote: Name of the remote (default: "origin").

        Returns:
            The remote URL string, or None if the remote does not exist.
        """
        try:
            repo = await self._repo()
            return await self._run(lambda r: r.remotes[remote].url, repo)
        except (IndexError, AttributeError, ValueError) as e:
            logger.debug("Could not get remote URL for %s: %s", self._path, e)
            return None

    async def current_branch(self) -> str | None:
        """Return the name of the current branch, or None if detached HEAD.

        Returns:
            Branch name string, or None if in detached HEAD state.
        """
        try:
            repo = await self._repo()
            return await self._run(lambda r: r.active_branch.name, repo)
        except TypeError:
            return None  # detached HEAD

    async def pull(self, remote: str = "origin", ff_only: bool = True) -> bool:
        """Pull from a remote, fast-forward only by default.

        Args:
            remote: Remote name to pull from.
            ff_only: If True, only allow fast-forward merges.

        Returns:
            True if pull succeeded, False on error.
        """
        def _pull(r: git.Repo) -> bool:
            try:
                kwargs: dict = {"ff_only": ff_only} if ff_only else {}
                r.remotes[remote].pull(**kwargs)
                return True
            except git.GitCommandError as e:
                logger.warning("git pull failed: %s", e)
                return False

        repo = await self._repo()
        async with self._write_lock:  # serialise writes on this repo path
            return await self._run(_pull, repo)

    async def is_dirty(self) -> bool:
        """Return True if the working tree has uncommitted changes.

        Returns:
            True if any tracked files are modified or staged.
        """
        repo = await self._repo()
        return await self._run(lambda r: r.is_dirty(), repo)

    async def get_head_sha(self) -> str:
        """Return the SHA of the current HEAD commit.

        Returns:
            Full 40-character SHA hex string.
        """
        repo = await self._repo()
        return await self._run(lambda r: r.head.commit.hexsha, repo)

    async def list_remotes(self) -> dict[str, str]:
        """Return a mapping of remote name → URL for all remotes.

        Returns:
            Dict where keys are remote names and values are URLs.
        """
        def _remotes(r: git.Repo) -> dict[str, str]:
            return {rem.name: rem.url for rem in r.remotes}

        repo = await self._repo()
        return await self._run(_remotes, repo)
