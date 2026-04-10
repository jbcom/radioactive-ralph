from __future__ import annotations

import pytest
import respx
import httpx
import os
from datetime import UTC, datetime

from radioactive_ralph.forge.base import ForgeInfo, ForgePR, PRCreateParams, CIState
from radioactive_ralph.forge.gitea import GiteaForge, AuthError, _discover_gitea_token, _parse_status_state

@pytest.fixture
def forge_info():
    return ForgeInfo(
        host="git.example.com",
        slug="org/repo",
        forge_type="gitea",
        api_base_url="https://git.example.com/api/v1"
    )

def test_discover_gitea_token(mocker):
    mocker.patch.dict(os.environ, {"GITEA_TOKEN": "test-gitea"})
    assert _discover_gitea_token() == "test-gitea"

    mocker.patch.dict(os.environ, {}, clear=True)
    mocker.patch.dict(os.environ, {"FORGEJO_TOKEN": "test-forgejo"})
    assert _discover_gitea_token() == "test-forgejo"

    mocker.patch.dict(os.environ, {}, clear=True)
    with pytest.raises(AuthError):
        _discover_gitea_token()

def test_parse_status_state():
    assert _parse_status_state("success") == CIState.SUCCESS
    assert _parse_status_state("failure") == CIState.FAILURE
    assert _parse_status_state("pending") == CIState.PENDING
    assert _parse_status_state("unknown") == CIState.UNKNOWN

@pytest.mark.asyncio
async def test_list_prs(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITEA_TOKEN": "test"})
    forge = GiteaForge(info=forge_info)

    pr_data = [{
        "number": 1,
        "title": "Fix bug",
        "user": {"login": "alice"},
        "head": {"label": "feature-branch", "sha": "12345"},
        "html_url": "https://git.example.com/org/repo/pulls/1",
        "updated_at": "2024-01-01T12:00:00Z"
    }]

    with respx.mock:
        respx.get("https://git.example.com/api/v1/repos/org/repo/pulls?state=open&limit=50").respond(json=pr_data)
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
    mocker.patch.dict(os.environ, {"GITEA_TOKEN": "test"})
    forge = GiteaForge(info=forge_info)
    pr = ForgePR(number=1, title="T", author="a", branch="b", head_sha="12345", is_draft=False, url="", updated_at=datetime.now(UTC))

    with respx.mock:
        # Success
        respx.get("https://git.example.com/api/v1/repos/org/repo/statuses/12345").respond(
            json=[{"status": "success", "context": "ci"}]
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.SUCCESS

        # Failure
        respx.get("https://git.example.com/api/v1/repos/org/repo/statuses/12345").respond(
            json=[{"status": "success", "context": "ci"}, {"status": "failure", "context": "lint"}]
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.FAILURE

        # Pending
        respx.get("https://git.example.com/api/v1/repos/org/repo/statuses/12345").respond(
            json=[{"status": "pending", "context": "ci"}]
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.PENDING
        
        # HTTP Error
        respx.get("https://git.example.com/api/v1/repos/org/repo/statuses/12345").respond(status_code=404)
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.UNKNOWN

    # Missing head_sha
    pr_no_sha = ForgePR(number=1, title="T", author="a", branch="b", head_sha="", is_draft=False, url="", updated_at=datetime.now(UTC))
    async with forge as client:
        ci = await client.get_pr_ci(pr_no_sha)
    assert ci.state == CIState.UNKNOWN

    # Running state
    with respx.mock:
        respx.get("https://git.example.com/api/v1/repos/org/repo/statuses/12345").respond(
            json=[{"status": "running", "context": "ci"}]
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        # The code maps RUNNING to PENDING in the summary
        assert ci.state == CIState.PENDING

    # Success/Skipped state
    with respx.mock:
        respx.get("https://git.example.com/api/v1/repos/org/repo/statuses/12345").respond(
            json=[{"status": "success", "context": "ci"}, {"status": "skipped", "context": "lint"}]
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.SUCCESS

    # Non-list response
    with respx.mock:
        respx.get("https://git.example.com/api/v1/repos/org/repo/statuses/12345").respond(
            json={"error": "not a list"}
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.UNKNOWN

    # Unknown/other state
    with respx.mock:
        respx.get("https://git.example.com/api/v1/repos/org/repo/statuses/12345").respond(
            json=[{"status": "some-weird-status", "context": "ci"}]
        )
        async with forge as client:
            ci = await client.get_pr_ci(pr)
        assert ci.state == CIState.UNKNOWN
@pytest.mark.asyncio
async def test_get_pr_reviews(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITEA_TOKEN": "test"})
    forge = GiteaForge(info=forge_info)
    pr = ForgePR(number=1, title="T", author="a", branch="b", head_sha="12345", is_draft=False, url="", updated_at=datetime.now(UTC))

    with respx.mock:
        respx.get("https://git.example.com/api/v1/repos/org/repo/pulls/1/reviews").respond(
            json=[
                {"user": {"id": 1}, "state": "COMMENT"},
                {"user": {"id": 1}, "state": "APPROVED"},
                {"user": {"id": 2}, "state": "REQUEST_CHANGES"}
            ]
        )
        async with forge as client:
            pr = await client.get_pr_reviews(pr)
            
    assert pr.review_count == 2
    assert pr.review_approved is True
    assert pr.changes_requested is True

@pytest.mark.asyncio
async def test_get_pr_reviews_errors(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITEA_TOKEN": "test"})
    forge = GiteaForge(info=forge_info)
    pr = ForgePR(number=1, title="T", author="a", branch="b", head_sha="12345", is_draft=False, url="", updated_at=datetime.now(UTC))

    with respx.mock:
        # Non-list response
        respx.get("https://git.example.com/api/v1/repos/org/repo/pulls/1/reviews").respond(json={"error": "failed"})
        async with forge as client:
            pr = await client.get_pr_reviews(pr)
        assert pr.review_count == 0

        # HTTP error
        respx.get("https://git.example.com/api/v1/repos/org/repo/pulls/1/reviews").respond(status_code=500)
        async with forge as client:
            pr = await client.get_pr_reviews(pr)
        assert pr.review_count == 0

@pytest.mark.asyncio
async def test_create_pr(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITEA_TOKEN": "test"})
    forge = GiteaForge(info=forge_info)
    params = PRCreateParams(title="Fix", body="Desc", head="feat", base="main")

    with respx.mock:
        respx.post("https://git.example.com/api/v1/repos/org/repo/pulls").respond(
            json={"number": 2, "title": "Fix"}
        )
        async with forge as client:
            pr = await client.create_pr(params)
            
    assert pr.number == 2
    assert pr.title == "Fix"

@pytest.mark.asyncio
async def test_merge_pr(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITEA_TOKEN": "test"})
    forge = GiteaForge(info=forge_info)
    pr = ForgePR(number=1, title="T", author="a", branch="b", head_sha="12345", is_draft=False, url="", updated_at=datetime.now(UTC))

    with respx.mock:
        respx.post("https://git.example.com/api/v1/repos/org/repo/pulls/1/merge").respond(status_code=200)
        async with forge as client:
            res = await client.merge_pr(pr)
    assert res is True

    with respx.mock:
        respx.post("https://git.example.com/api/v1/repos/org/repo/pulls/1/merge").respond(status_code=400)
        async with forge as client:
            res = await client.merge_pr(pr)
    assert res is False

@pytest.mark.asyncio
async def test_get_pr_diff(forge_info, mocker):
    mocker.patch.dict(os.environ, {"GITEA_TOKEN": "test"})
    forge = GiteaForge(info=forge_info)
    pr = ForgePR(number=1, title="T", author="a", branch="b", head_sha="", is_draft=False, url="https://git.example.com/org/repo/pulls/1", updated_at=datetime.now(UTC))

    with respx.mock:
        respx.get("https://git.example.com/org/repo/pulls/1.diff").respond(text="diff --git a/b b/b")
        async with forge as client:
            diff = await client.get_pr_diff(pr)
            
    assert diff == "diff --git a/b b/b"
