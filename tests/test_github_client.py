import os
import subprocess

import httpx
import pytest
import respx

from radioactive_ralph.github_client import (
    AuthError,
    GitHubClient,
    get_github_token,
    inside_claude_code,
)


def test_inside_claude_code(mocker):
    mocker.patch.dict(os.environ, {"CLAUDECODE": "1"})
    assert inside_claude_code() is True

    mocker.patch.dict(os.environ, {"CLAUDECODE": "0"})
    assert inside_claude_code() is False


def test_get_github_token_env(mocker):
    mocker.patch.dict(os.environ, {"GH_TOKEN": "test-gh-token"})
    assert get_github_token() == "test-gh-token"

    mocker.patch.dict(os.environ, {}, clear=True)
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-github-token"})
    assert get_github_token() == "test-github-token"


def test_get_github_token_cli(mocker):
    mocker.patch.dict(os.environ, {}, clear=True)
    
    mock_run = mocker.patch("subprocess.run")
    mock_run.return_value.returncode = 0
    mock_run.return_value.stdout = "cli-token\n"
    
    assert get_github_token() == "cli-token"
    mock_run.assert_called_once_with(
        ["gh", "auth", "token"],
        capture_output=True, text=True, timeout=5
    )


def test_get_github_token_fail(mocker):
    mocker.patch.dict(os.environ, {}, clear=True)
    
    mock_run = mocker.patch("subprocess.run")
    mock_run.side_effect = FileNotFoundError()
    
    with pytest.raises(AuthError, match="install and authenticate"):
        get_github_token()


def test_get_github_token_fail_claude(mocker):
    mocker.patch.dict(os.environ, {"CLAUDECODE": "1"}, clear=True)
    
    mock_run = mocker.patch("subprocess.run")
    mock_run.side_effect = FileNotFoundError()
    
    with pytest.raises(AuthError, match="Running inside Claude Code"):
        get_github_token()


@pytest.mark.asyncio
async def test_client_context_manager(mocker):
    mocker.patch.dict(os.environ, {"GH_TOKEN": "test-token"})
    client = GitHubClient()
    
    with pytest.raises(AssertionError):
        client._c()
        
    async with client as c:
        assert isinstance(c._c(), httpx.AsyncClient)
        assert c._c().headers["Authorization"] == "Bearer test-token"
        
    assert client._client is None


@pytest.mark.asyncio
async def test_get(mocker):
    mocker.patch.dict(os.environ, {"GH_TOKEN": "test-token"})
    client = GitHubClient()
    
    with respx.mock:
        respx.get("https://api.github.com/test?foo=bar").respond(json={"success": True})
        
        async with client as c:
            result = await c.get("/test", foo="bar")
            
    assert result == {"success": True}


@pytest.mark.asyncio
async def test_post(mocker):
    mocker.patch.dict(os.environ, {"GH_TOKEN": "test-token"})
    client = GitHubClient()
    
    with respx.mock:
        respx.post("https://api.github.com/test").respond(json={"id": 1})
        
        async with client as c:
            result = await c.post("/test", json={"foo": "bar"})
            
    assert result == {"id": 1}


@pytest.mark.asyncio
async def test_get_paginated(mocker):
    mocker.patch.dict(os.environ, {"GH_TOKEN": "test-token"})
    client = GitHubClient()
    
    with respx.mock:
        # Page 1
        respx.get("https://api.github.com/test?per_page=100").respond(
            json=[{"id": 1}],
            headers={"link": '<https://api.github.com/test?page=2>; rel="next"'}
        )
        # Page 2
        respx.get("https://api.github.com/test?page=2").respond(
            json=[{"id": 2}]
        )
        
        async with client as c:
            results = await c.get_paginated("/test")
            
    assert len(results) == 2
    assert results[0]["id"] == 1
    assert results[1]["id"] == 2


@pytest.mark.asyncio
async def test_get_paginated_dict_items(mocker):
    mocker.patch.dict(os.environ, {"GH_TOKEN": "test-token"})
    client = GitHubClient()
    
    with respx.mock:
        respx.get("https://api.github.com/test?per_page=100").respond(
            json={"items": [{"id": 1}]}
        )
        
        async with client as c:
            results = await c.get_paginated("/test")
            
    assert len(results) == 1
    assert results[0]["id"] == 1


@pytest.mark.asyncio
async def test_get_paginated_dict_other(mocker):
    mocker.patch.dict(os.environ, {"GH_TOKEN": "test-token"})
    client = GitHubClient()
    
    with respx.mock:
        respx.get("https://api.github.com/test?per_page=100").respond(
            json={"id": 1}
        )
        
        async with client as c:
            results = await c.get_paginated("/test")
            
    assert len(results) == 1
    assert results[0] == {"id": 1}


@pytest.mark.asyncio
async def test_list_prs(mocker):
    mocker.patch.dict(os.environ, {"GH_TOKEN": "test-token"})
    client = GitHubClient()
    
    with respx.mock:
        respx.get("https://api.github.com/repos/org/repo/pulls?state=open&per_page=100").respond(
            json=[{"number": 1}]
        )
        
        async with client as c:
            prs = await c.list_prs("org/repo")
            
    assert prs == [{"number": 1}]


@pytest.mark.asyncio
async def test_get_pr_checks(mocker):
    mocker.patch.dict(os.environ, {"GH_TOKEN": "test-token"})
    client = GitHubClient()
    
    with respx.mock:
        respx.get("https://api.github.com/repos/org/repo/commits/123/check-runs?per_page=100").respond(
            json={"check_runs": [{"id": 1}]}
        )
        
        async with client as c:
            checks = await c.get_pr_checks("org/repo", "123")
            
    # The client's get_paginated logic does not know about "check_runs" key natively,
    # so it falls through to returning the whole dict inside a list
    # `elif isinstance(data, dict) and "items" in data: ... else: return [data]`
    assert len(checks) == 1
    assert "check_runs" in checks[0]


@pytest.mark.asyncio
async def test_merge_pr(mocker):
    mocker.patch.dict(os.environ, {"GH_TOKEN": "test-token"})
    client = GitHubClient()
    
    with respx.mock:
        respx.post("https://api.github.com/repos/org/repo/pulls/1/merge").respond(status_code=200, json={})
        
        async with client as c:
            result = await c.merge_pr("org/repo", 1)
            
    assert result is True

    with respx.mock:
        respx.post("https://api.github.com/repos/org/repo/pulls/1/merge").respond(status_code=400)
        
        async with client as c:
            result = await c.merge_pr("org/repo", 1)
            
    assert result is False
