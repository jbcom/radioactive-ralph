from __future__ import annotations

import pytest
import os

from radioactive_ralph.forge import detect_forge, get_forge_client
from radioactive_ralph.forge.gitea import GiteaForge
from radioactive_ralph.forge.github import GitHubForge
from radioactive_ralph.forge.gitlab import GitLabForge
from radioactive_ralph.forge.base import ForgeInfo, ForgePR
from datetime import UTC, datetime, timedelta
import httpx

def test_forge_info_properties():
    info = ForgeInfo(host="h", slug="owner/repo", forge_type="g", api_base_url="u")
    assert info.owner == "owner"
    assert info.repo == "repo"


def test_forge_ci_failed():
    from radioactive_ralph.forge.base import ForgeCI, CIState
    ci = ForgeCI(state=CIState.FAILURE)
    assert ci.failed is True
    ci.state = CIState.CANCELLED
    assert ci.failed is True
    ci.state = CIState.SUCCESS
    assert ci.failed is False


def test_forge_pr_is_stale():
    now = datetime.now(UTC)
    pr = ForgePR(
        number=1, title="t", author="a", branch="b", head_sha="s",
        is_draft=False, url="u", updated_at=now - timedelta(days=8)
    )
    assert pr.is_stale is True
    
    pr.updated_at = now
    assert pr.is_stale is False


@pytest.mark.asyncio
async def test_forge_client_close(mocker):
    from radioactive_ralph.forge.github import GitHubForge
    from radioactive_ralph.forge.base import ForgeInfo
    
    info = ForgeInfo(host="h", slug="o/r", forge_type="github", api_base_url="u")
    mocker.patch("radioactive_ralph.forge.github.get_github_token", return_value="t")
    
    # Test closing internal client
    forge = GitHubForge(info)
    async with forge as client:
        internal_http = client._http
        assert internal_http is not None
    assert forge._http is None
    
    # Test NOT closing external client
    mock_http = mocker.AsyncMock(spec=httpx.AsyncClient)
    forge = GitHubForge(info, http_client=mock_http)
    async with forge as client:
        assert client._http is mock_http
    assert forge._http is mock_http
    mock_http.aclose.assert_not_called()


def test_detect_forge_github():
    info = detect_forge("git@github.com:jbcom/radioactive-ralph.git")
    assert info.host == "github.com"
    assert info.slug == "jbcom/radioactive-ralph"
    assert info.forge_type == "github"
    assert info.api_base_url == "https://api.github.com"

    info = detect_forge("https://github.com/jbcom/radioactive-ralph.git")
    assert info.host == "github.com"
    assert info.forge_type == "github"

def test_detect_forge_github_enterprise():
    os.environ["FORGE_TYPE_OVERRIDE"] = "github"
    info = detect_forge("https://git.company.com/org/repo")
    assert info.host == "git.company.com"
    assert info.forge_type == "github"
    assert info.api_base_url == "https://git.company.com/api/v3"
    del os.environ["FORGE_TYPE_OVERRIDE"]

def test_detect_forge_gitlab():
    info = detect_forge("git@gitlab.com:org/repo.git")
    assert info.host == "gitlab.com"
    assert info.forge_type == "gitlab"
    assert info.api_base_url == "https://gitlab.com/api/v4"

    info = detect_forge("https://gitlab.example.com/org/repo")
    assert info.host == "gitlab.example.com"
    assert info.forge_type == "gitlab"
    assert info.api_base_url == "https://gitlab.example.com/api/v4"

def test_detect_forge_gitea():
    info = detect_forge("https://git.example.com/org/repo")
    assert info.host == "git.example.com"
    assert info.forge_type == "gitea"
    assert info.api_base_url == "https://git.example.com/api/v1"

def test_detect_forge_override():
    os.environ["FORGE_TYPE_OVERRIDE"] = "gitlab"
    info = detect_forge("https://git.custom.com/org/repo")
    assert info.forge_type == "gitlab"
    assert info.api_base_url == "https://git.custom.com/api/v4"
    del os.environ["FORGE_TYPE_OVERRIDE"]

def test_detect_forge_invalid():
    with pytest.raises(ValueError):
        detect_forge("not-a-url")

def test_get_forge_client(mocker):
    # Mock tokens so initialization doesn't fail
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test", "GITLAB_TOKEN": "test", "GITEA_TOKEN": "test"})
    
    client = get_forge_client("https://github.com/org/repo")
    assert isinstance(client, GitHubForge)

    client = get_forge_client("https://gitlab.com/org/repo")
    assert isinstance(client, GitLabForge)

    client = get_forge_client("https://git.example.com/org/repo")
    assert isinstance(client, GiteaForge)
