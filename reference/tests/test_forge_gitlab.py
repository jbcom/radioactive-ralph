import os
from datetime import UTC, datetime

import httpx
import pytest
import respx

from radioactive_ralph.forge.base import CIState, ForgeInfo, ForgePR, PRCreateParams
from radioactive_ralph.forge.gitlab import (
    AuthError,
    GitLabForge,
    _discover_gitlab_token,
    _parse_pipeline_state,
)


@pytest.fixture
def forge_info():
    return ForgeInfo(
        host="gitlab.com",
        slug="org/repo",
        forge_type="gitlab",
        api_base_url="https://gitlab.com/api/v4",
    )


def test_discover_gitlab_token_env(mocker):
    mocker.patch.dict(os.environ, {"GITLAB_TOKEN": "test-token"})
    assert _discover_gitlab_token() == "test-token"

    mocker.patch.dict(os.environ, {}, clear=True)
    mocker.patch.dict(os.environ, {"GL_TOKEN": "test-gl-token"})
    assert _discover_gitlab_token() == "test-gl-token"


def test_discover_gitlab_token_cli(mocker):
    mocker.patch.dict(os.environ, {}, clear=True)

    mock_run = mocker.patch("subprocess.run")
    mock_run.return_value.returncode = 0
    mock_run.return_value.stdout = "Token: cli-token\n"

    assert _discover_gitlab_token() == "cli-token"
    mock_run.assert_called_once_with(
        ["glab", "auth", "status", "--show-token"], capture_output=True, text=True, timeout=5
    )


def test_discover_gitlab_token_fail(mocker):
    mocker.patch.dict(os.environ, {}, clear=True)

    mock_run = mocker.patch("subprocess.run")
    mock_run.side_effect = FileNotFoundError()

    with pytest.raises(AuthError, match="No GitLab token found"):
        _discover_gitlab_token()


def test_parse_pipeline_state():
    assert _parse_pipeline_state("success") == CIState.SUCCESS
    assert _parse_pipeline_state("failed") == CIState.FAILURE
    assert _parse_pipeline_state("canceled") == CIState.CANCELLED
    assert _parse_pipeline_state("skipped") == CIState.SKIPPED
    assert _parse_pipeline_state("running") == CIState.RUNNING
    assert _parse_pipeline_state("pending") == CIState.PENDING
    assert _parse_pipeline_state("created") == CIState.PENDING
    assert _parse_pipeline_state("waiting_for_resource") == CIState.PENDING
    assert _parse_pipeline_state("unknown_state") == CIState.UNKNOWN


@pytest.mark.asyncio
async def test_forge_init_and_http_client(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITLAB_TOKEN": "test-token"})
    forge = GitLabForge(info=forge_info)
    assert forge._token == "test-token"
    assert forge._encoded_slug == "org%2Frepo"

    with pytest.raises(AssertionError):
        forge._c()

    async with forge as client:
        assert isinstance(client._c(), httpx.AsyncClient)
        assert client._c().headers["PRIVATE-TOKEN"] == "test-token"


@pytest.mark.asyncio
async def test_post_put_invalid_json(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITLAB_TOKEN": "test-token"})
    forge = GitLabForge(info=forge_info)

    with respx.mock:
        respx.post("https://gitlab.com/api/v4/test").respond(text="not json")
        respx.put("https://gitlab.com/api/v4/test").respond(text="not json")

        async with forge as client:
            post_result = await client._post("/test", json={"data": 1})
            put_result = await client._put("/test", json={"data": 1})

    assert post_result == {}
    assert put_result == {}


@pytest.mark.asyncio
async def test_list_prs(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITLAB_TOKEN": "test-token"})
    forge = GitLabForge(info=forge_info)

    pr_data = [
        {
            "iid": 1,
            "title": "Fix bug",
            "author": {"username": "alice"},
            "source_branch": "feature-branch",
            "sha": "12345",
            "web_url": "https://gitlab.com/org/repo/-/merge_requests/1",
            "updated_at": "2024-01-01T12:00:00Z",
            "draft": False,
        }
    ]

    with respx.mock:
        respx.get(
            "https://gitlab.com/api/v4/projects/org%2Frepo/merge_requests?state=opened&per_page=100"
        ).respond(json=pr_data)
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
    mocker.patch.dict(os.environ, {"GITLAB_TOKEN": "test-token"})
    forge = GitLabForge(info=forge_info)

    pr_no_sha = ForgePR(
        number=1,
        title="T",
        author="a",
        branch="b",
        head_sha="",
        is_draft=False,
        url="",
        updated_at=datetime.now(UTC),
    )
    async with forge as client:
        ci = await client.get_pr_ci(pr_no_sha)
    assert ci.state == CIState.UNKNOWN

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
        # Success
        respx.get(
            "https://gitlab.com/api/v4/projects/org%2Frepo/pipelines?sha=12345&per_page=5"
        ).respond(json=[{"status": "success", "id": 100}])
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.SUCCESS

        # Empty response
        respx.get(
            "https://gitlab.com/api/v4/projects/org%2Frepo/pipelines?sha=12345&per_page=5"
        ).respond(json=[])
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.UNKNOWN

        # HTTP Error
        respx.get(
            "https://gitlab.com/api/v4/projects/org%2Frepo/pipelines?sha=12345&per_page=5"
        ).respond(status_code=404)
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.UNKNOWN


@pytest.mark.asyncio
async def test_get_pr_reviews(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITLAB_TOKEN": "test-token"})
    forge = GitLabForge(info=forge_info)
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
        respx.get(
            "https://gitlab.com/api/v4/projects/org%2Frepo/merge_requests/1/approvals"
        ).respond(
            json={"approved_by": [{"user": {"id": 1}}, {"user": {"id": 2}}], "approved": True}
        )
        async with forge as client:
            pr = await client.get_pr_reviews(pr)

    assert pr.review_count == 2
    assert pr.review_approved is True

    with respx.mock:
        respx.get(
            "https://gitlab.com/api/v4/projects/org%2Frepo/merge_requests/1/approvals"
        ).respond(status_code=404)
        async with forge as client:
            pr2 = await client.get_pr_reviews(pr)
    assert pr2 == pr


@pytest.mark.asyncio
async def test_create_pr(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITLAB_TOKEN": "test-token"})
    forge = GitLabForge(info=forge_info)
    params = PRCreateParams(title="Fix", body="Desc", head="feat", base="main", draft=True)

    with respx.mock:
        respx.post("https://gitlab.com/api/v4/projects/org%2Frepo/merge_requests").respond(
            json={"iid": 2, "title": "Draft: Fix", "source_branch": "feat", "draft": True}
        )
        async with forge as client:
            pr = await client.create_pr(params)

    assert pr.number == 2
    assert pr.title == "Draft: Fix"
    assert pr.is_draft is True


@pytest.mark.asyncio
async def test_merge_pr(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITLAB_TOKEN": "test-token"})
    forge = GitLabForge(info=forge_info)
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
        respx.put("https://gitlab.com/api/v4/projects/org%2Frepo/merge_requests/1/merge").respond(
            status_code=200, json={}
        )
        async with forge as client:
            res = await client.merge_pr(pr)
    assert res is True

    with respx.mock:
        respx.put("https://gitlab.com/api/v4/projects/org%2Frepo/merge_requests/1/merge").respond(
            status_code=400
        )
        async with forge as client:
            res = await client.merge_pr(pr)
    assert res is False


@pytest.mark.asyncio
async def test_get_pr_diff(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITLAB_TOKEN": "test-token"})
    forge = GitLabForge(info=forge_info)
    pr = ForgePR(
        number=1,
        title="T",
        author="a",
        branch="b",
        head_sha="",
        is_draft=False,
        url="https://gitlab.com/org/repo/-/merge_requests/1",
        updated_at=datetime.now(UTC),
    )

    with respx.mock:
        respx.get("https://gitlab.com/org/repo/-/merge_requests/1.diff").respond(
            text="diff --git a/b b/b"
        )
        async with forge as client:
            diff = await client.get_pr_diff(pr)

    assert diff == "diff --git a/b b/b"
