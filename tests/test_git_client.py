from pathlib import Path

import git
import pytest

from radioactive_ralph.git_client import GitClient, _open_repo


def test_open_repo_success(mocker):
    mock_repo = mocker.patch("git.Repo")
    _open_repo("/fake/path")
    mock_repo.assert_called_once_with("/fake/path", search_parent_directories=True)


def test_open_repo_invalid_git(mocker):
    mocker.patch("git.Repo", side_effect=git.InvalidGitRepositoryError)
    with pytest.raises(ValueError, match="Not a git repository: /fake/path"):
        _open_repo("/fake/path")


def test_open_repo_no_such_path(mocker):
    mocker.patch("git.Repo", side_effect=git.NoSuchPathError)
    with pytest.raises(ValueError, match="Not a git repository: /fake/path"):
        _open_repo("/fake/path")


@pytest.fixture
def mock_repo(mocker):
    return mocker.Mock(spec=git.Repo)


@pytest.fixture
def git_client():
    return GitClient("/fake/path")


@pytest.mark.asyncio
async def test_repo_cache(git_client, mocker, mock_repo):
    mock_open = mocker.patch("radioactive_ralph.git_client._open_repo", return_value=mock_repo)
    repo1 = await git_client._repo()
    repo2 = await git_client._repo()
    assert repo1 is repo2
    mock_open.assert_called_once_with(Path("/fake/path"))


@pytest.mark.asyncio
async def test_get_remote_url_success(git_client, mocker, mock_repo):
    mocker.patch.object(git_client, "_repo", return_value=mock_repo)
    mock_remote = mocker.Mock()
    mock_remote.url = "https://github.com/org/repo.git"
    mock_repo.remotes = {"origin": mock_remote}

    url = await git_client.get_remote_url()
    assert url == "https://github.com/org/repo.git"


@pytest.mark.asyncio
async def test_get_remote_url_error(git_client, mocker, mock_repo):
    mocker.patch.object(git_client, "_repo", return_value=mock_repo)
    mock_repo.remotes = {}  # This will raise an error when trying to access "origin"

    url = await git_client.get_remote_url()
    assert url is None


@pytest.mark.asyncio
async def test_current_branch_success(git_client, mocker, mock_repo):
    mocker.patch.object(git_client, "_repo", return_value=mock_repo)
    mock_branch = mocker.Mock()
    mock_branch.name = "main"
    mock_repo.active_branch = mock_branch

    branch = await git_client.current_branch()
    assert branch == "main"


@pytest.mark.asyncio
async def test_current_branch_detached(git_client, mocker, mock_repo):
    mocker.patch.object(git_client, "_repo", return_value=mock_repo)
    type(mock_repo).active_branch = mocker.PropertyMock(side_effect=TypeError)

    branch = await git_client.current_branch()
    assert branch is None


@pytest.mark.asyncio
async def test_pull_success(git_client, mocker, mock_repo):
    mocker.patch.object(git_client, "_repo", return_value=mock_repo)
    mock_remote = mocker.Mock()
    mock_repo.remote.return_value = mock_remote

    result = await git_client.pull()
    assert result is True
    mock_repo.remote.assert_called_once_with("origin")
    mock_remote.pull.assert_called_once_with(ff_only=True)


@pytest.mark.asyncio
async def test_pull_no_ff(git_client, mocker, mock_repo):
    mocker.patch.object(git_client, "_repo", return_value=mock_repo)
    mock_remote = mocker.Mock()
    mock_repo.remote.return_value = mock_remote

    result = await git_client.pull(ff_only=False)
    assert result is True
    mock_remote.pull.assert_called_once_with()


@pytest.mark.asyncio
async def test_pull_error(git_client, mocker, mock_repo):
    mocker.patch.object(git_client, "_repo", return_value=mock_repo)
    mock_remote = mocker.Mock()
    mock_remote.pull.side_effect = git.GitCommandError("pull", 1)
    mock_repo.remote.return_value = mock_remote

    result = await git_client.pull()
    assert result is False


@pytest.mark.asyncio
async def test_is_dirty(git_client, mocker, mock_repo):
    mocker.patch.object(git_client, "_repo", return_value=mock_repo)
    mock_repo.is_dirty.return_value = True

    is_dirty = await git_client.is_dirty()
    assert is_dirty is True
    mock_repo.is_dirty.assert_called_once()


@pytest.mark.asyncio
async def test_get_head_sha(git_client, mocker, mock_repo):
    mocker.patch.object(git_client, "_repo", return_value=mock_repo)
    mock_commit = mocker.Mock()
    mock_commit.hexsha = "abcdef1234567890abcdef1234567890abcdef12"
    mock_head = mocker.Mock()
    mock_head.commit = mock_commit
    mock_repo.head = mock_head

    sha = await git_client.get_head_sha()
    assert sha == "abcdef1234567890abcdef1234567890abcdef12"


@pytest.mark.asyncio
async def test_list_remotes(git_client, mocker, mock_repo):
    mocker.patch.object(git_client, "_repo", return_value=mock_repo)

    remote1 = mocker.Mock()
    remote1.name = "origin"
    remote1.url = "git@github.com:org/repo1.git"

    remote2 = mocker.Mock()
    remote2.name = "upstream"
    remote2.url = "https://github.com/org/repo2.git"

    mock_repo.remotes = [remote1, remote2]

    remotes = await git_client.list_remotes()
    assert remotes == {
        "origin": "git@github.com:org/repo1.git",
        "upstream": "https://github.com/org/repo2.git",
    }
