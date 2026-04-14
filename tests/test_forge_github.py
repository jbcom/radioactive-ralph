import os
import subprocess
from datetime import UTC, datetime

import httpx
import pytest
import respx

from radioactive_ralph.forge.base import CIState, ForgeInfo, ForgePR, PRCreateParams
from radioactive_ralph.forge.github import (
    GitHubForge,
    _parse_ci_state,
    _parse_commit_status,
)


@pytest.fixture
def forge_info():
    return ForgeInfo(
        host="github.com",
        slug="org/repo",
        forge_type="github",
        api_base_url="https://api.github.com",
    )


def test_parse_commit_status():
    assert _parse_commit_status("success") == CIState.SUCCESS
    assert _parse_commit_status("failure") == CIState.FAILURE
    assert _parse_commit_status("error") == CIState.FAILURE
    assert _parse_commit_status("pending") == CIState.PENDING
    assert _parse_commit_status("unknown_state") == CIState.UNKNOWN


def test_parse_ci_state():
    assert _parse_ci_state(None, "in_progress") == CIState.RUNNING
    assert _parse_ci_state(None, "queued") == CIState.PENDING
    assert _parse_ci_state("success", "completed") == CIState.SUCCESS
    assert _parse_ci_state("failure", "completed") == CIState.FAILURE
    assert _parse_ci_state("cancelled", "completed") == CIState.CANCELLED
    assert _parse_ci_state("skipped", "completed") == CIState.SKIPPED
    assert _parse_ci_state("timed_out", "completed") == CIState.FAILURE
    assert _parse_ci_state("action_required", "completed") == CIState.FAILURE
    assert _parse_ci_state("unknown_conclusion", "completed") == CIState.UNKNOWN


@pytest.mark.asyncio
async def test_forge_init_and_http_client(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)
    assert forge._token == "test-token"

    with pytest.raises(AssertionError):
        forge._c()  # Should assert when not in context manager

    async with forge as client:
        assert isinstance(client._c(), httpx.AsyncClient)
        assert client._c().headers["Authorization"] == "Bearer test-token"


def test_discover_github_token_errors(mocker):
    from radioactive_ralph.forge.auth import AuthError, get_github_token

    # 1. No token at all, gh CLI not on PATH
    mocker.patch.dict(os.environ, {}, clear=True)
    mocker.patch("radioactive_ralph.forge.auth.shutil.which", return_value=None)
    with pytest.raises(AuthError) as exc:
        get_github_token()
    assert "No GitHub token found" in str(exc.value)

    # 2. No token, gh exists but auth subprocess times out
    mocker.patch("radioactive_ralph.forge.auth.shutil.which", return_value="/usr/bin/gh")
    mocker.patch("subprocess.run", side_effect=subprocess.TimeoutExpired(cmd="gh", timeout=5))
    with pytest.raises(AuthError) as exc:
        get_github_token()
    assert "No GitHub token found" in str(exc.value)

    # 3. Inside Claude Code hint
    mocker.patch.dict(os.environ, {"CLAUDECODE": "1"})
    with pytest.raises(AuthError) as exc:
        get_github_token()
    assert "GitHub MCP server" in str(exc.value)


@pytest.mark.asyncio
async def test_get_paginated(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)

    with respx.mock:
        # Page 1
        respx.get("https://api.github.com/test?per_page=100").respond(
            json=[{"id": 1}], headers={"link": '<https://api.github.com/test?page=2>; rel="next"'}
        )
        # Page 2
        respx.get("https://api.github.com/test?page=2").respond(json=[{"id": 2}])

        async with forge as client:
            results = await client._get_paginated("/test")

    assert len(results) == 2
    assert results[0]["id"] == 1
    assert results[1]["id"] == 2


@pytest.mark.asyncio
async def test_get_paginated_dict_items(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)

    with respx.mock:
        respx.get("https://api.github.com/test?per_page=100").respond(json={"items": [{"id": 1}]})

        async with forge as client:
            results = await client._get_paginated("/test")

    assert len(results) == 1
    assert results[0]["id"] == 1


@pytest.mark.asyncio
async def test_get_paginated_dict_check_runs(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)

    with respx.mock:
        respx.get("https://api.github.com/test?per_page=100").respond(
            json={"check_runs": [{"id": 1}]}
        )

        async with forge as client:
            results = await client._get_paginated("/test")

    assert len(results) == 1
    assert results[0]["id"] == 1


@pytest.mark.asyncio
async def test_get_paginated_other(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)

    with respx.mock:
        respx.get("https://api.github.com/test?per_page=100").respond(json={"id": 1})

        async with forge as client:
            results = await client._get_paginated("/test")

    assert len(results) == 1
    assert results[0]["id"] == 1


@pytest.mark.asyncio
async def test_post_invalid_json(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)

    with respx.mock:
        respx.post("https://api.github.com/test").respond(text="not json")

        async with forge as client:
            result = await client._post("/test", json={"data": 1})

    assert result == {}


@pytest.mark.asyncio
async def test_list_prs(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)

    pr_data = [
        {
            "number": 1,
            "title": "Fix bug",
            "user": {"login": "alice"},
            "head": {"ref": "feature-branch", "sha": "12345"},
            "html_url": "https://github.com/org/repo/pull/1",
            "updated_at": "2024-01-01T12:00:00Z",
            "draft": False,
        }
    ]

    with respx.mock:
        respx.get("https://api.github.com/repos/org/repo/pulls?state=open&per_page=100").respond(
            json=pr_data
        )
        async with forge as client:
            prs = await client.list_prs()

    assert len(prs) == 1
    assert prs[0].number == 1
    assert prs[0].title == "Fix bug"
    assert prs[0].author == "alice"
    assert prs[0].branch == "feature-branch"
    assert prs[0].head_sha == "12345"


@pytest.mark.asyncio
async def test_get_pr_ci(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)
    pr = ForgePR(
        number=1,
        title="T",
        author="a",
        branch="b",
        head_sha="12345",
        is_draft=False,
        url="",
        updated_at=datetime.now(UTC),
    )

    with respx.mock:
        # Test success case
        respx.get(
            "https://api.github.com/repos/org/repo/commits/12345/check-runs?per_page=100"
        ).respond(
            json={"check_runs": [{"name": "lint", "status": "completed", "conclusion": "success"}]}
        )
        respx.get("https://api.github.com/repos/org/repo/commits/12345/status").respond(
            json={"statuses": [{"context": "ci", "state": "success"}]}
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.SUCCESS

        # Test failure case
        respx.get(
            "https://api.github.com/repos/org/repo/commits/12345/check-runs?per_page=100"
        ).respond(
            json={"check_runs": [{"name": "lint", "status": "completed", "conclusion": "success"}]}
        )
        respx.get("https://api.github.com/repos/org/repo/commits/12345/status").respond(
            json={"statuses": [{"context": "ci", "state": "failure"}]}
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.FAILURE

        # Test pending case
        respx.get(
            "https://api.github.com/repos/org/repo/commits/12345/check-runs?per_page=100"
        ).respond(
            json={"check_runs": [{"name": "lint", "status": "in_progress", "conclusion": None}]}
        )
        respx.get("https://api.github.com/repos/org/repo/commits/12345/status").respond(
            json={"statuses": []}
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.PENDING

        # Test skipped case
        respx.get(
            "https://api.github.com/repos/org/repo/commits/12345/check-runs?per_page=100"
        ).respond(
            json={"check_runs": [{"name": "lint", "status": "completed", "conclusion": "skipped"}]}
        )
        respx.get("https://api.github.com/repos/org/repo/commits/12345/status").respond(
            json={"statuses": []}
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.SUCCESS

        # Test unknown/other case
        respx.get(
            "https://api.github.com/repos/org/repo/commits/12345/check-runs?per_page=100"
        ).respond(
            json={
                "check_runs": [
                    {"name": "lint", "status": "completed", "conclusion": "unknown_conclusion"}
                ]
            }
        )
        respx.get("https://api.github.com/repos/org/repo/commits/12345/status").respond(
            json={"statuses": []}
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.UNKNOWN

        # Test unknown/empty case
        respx.get(
            "https://api.github.com/repos/org/repo/commits/12345/check-runs?per_page=100"
        ).respond(json={"check_runs": []})
        respx.get("https://api.github.com/repos/org/repo/commits/12345/status").respond(
            json={"statuses": []}
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.UNKNOWN


@pytest.mark.asyncio
async def test_get_pr_reviews(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)
    pr = ForgePR(
        number=1,
        title="T",
        author="a",
        branch="b",
        head_sha="12345",
        is_draft=False,
        url="",
        updated_at=datetime.now(UTC),
    )

    with respx.mock:
        respx.get("https://api.github.com/repos/org/repo/pulls/1/reviews").respond(
            json=[
                {"user": {"login": "user1"}, "state": "COMMENT"},
                {"user": {"login": "user1"}, "state": "APPROVED"},
                {"user": {"login": "user2"}, "state": "CHANGES_REQUESTED"},
            ]
        )
        async with forge as client:
            pr = await client.get_pr_reviews(pr)

    assert pr.review_count == 2
    assert pr.review_approved is True
    assert pr.changes_requested is True

    # Test non-list review response
    with respx.mock:
        respx.get("https://api.github.com/repos/org/repo/pulls/1/reviews").respond(
            json={"error": "failed"}
        )
        async with forge as client:
            pr = await client.get_pr_reviews(pr)
        assert pr.review_count == 2  # Remains unchanged from previous call


@pytest.mark.asyncio
async def test_create_pr(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)
    params = PRCreateParams(title="Fix", body="Desc", head="feat", base="main")

    with respx.mock:
        respx.post("https://api.github.com/repos/org/repo/pulls").respond(
            json={
                "number": 2,
                "title": "Fix",
                "head": {"ref": "feat", "sha": "111"},
                "draft": False,
            }
        )
        async with forge as client:
            pr = await client.create_pr(params)

    assert pr.number == 2
    assert pr.title == "Fix"


@pytest.mark.asyncio
async def test_merge_pr(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)
    pr = ForgePR(
        number=1,
        title="T",
        author="a",
        branch="b",
        head_sha="12345",
        is_draft=False,
        url="",
        updated_at=datetime.now(UTC),
    )

    with respx.mock:
        respx.post("https://api.github.com/repos/org/repo/pulls/1/merge").respond(
            status_code=200, json={}
        )
        async with forge as client:
            res = await client.merge_pr(pr)
    assert res is True

    with respx.mock:
        respx.post("https://api.github.com/repos/org/repo/pulls/1/merge").respond(status_code=400)
        async with forge as client:
            res = await client.merge_pr(pr)
    assert res is False


@pytest.mark.asyncio
async def test_get_pr_diff(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"})
    forge = GitHubForge(info=forge_info)
    pr = ForgePR(
        number=1,
        title="T",
        author="a",
        branch="b",
        head_sha="",
        is_draft=False,
        url="",
        updated_at=datetime.now(UTC),
    )

    with respx.mock:
        respx.get("https://api.github.com/repos/org/repo/pulls/1").respond(
            text="diff --git a/b b/b"
        )
        async with forge as client:
            diff = await client.get_pr_diff(pr)

    assert diff == "diff --git a/b b/b"
