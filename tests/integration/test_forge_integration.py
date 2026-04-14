"""Integration tests for forge clients using mocks."""

from __future__ import annotations

from datetime import UTC, datetime
from unittest.mock import AsyncMock, MagicMock

import pytest

from radioactive_ralph.forge.base import CIState, ForgeInfo, ForgePR
from radioactive_ralph.forge.github import GitHubForge


@pytest.mark.asyncio
async def test_github_forge_list_prs_integration(mocker) -> None:
    """Test listing PRs with mocked responses."""
    info = ForgeInfo(
        host="github.com",
        slug="org/repo",
        forge_type="github",
        api_base_url="https://api.github.com",
    )

    # Mock the HTTP client
    mock_resp1 = MagicMock()
    mock_resp1.json.return_value = [
        {
            "number": 1,
            "title": "PR 1",
            "user": {"login": "user1"},
            "head": {"ref": "feat/1", "sha": "abc"},
            "html_url": "url1",
            "updated_at": "2026-04-10T10:00:00Z",
        }
    ]
    mock_resp1.headers = {
        "link": '<https://api.github.com/repos/org/repo/pulls?page=2>; rel="next"'
    }
    mock_resp1.raise_for_status = MagicMock()

    mock_resp2 = MagicMock()
    mock_resp2.json.return_value = [
        {
            "number": 2,
            "title": "PR 2",
            "user": {"login": "user2"},
            "head": {"ref": "feat/2", "sha": "def"},
            "html_url": "url2",
            "updated_at": "2026-04-10T11:00:00Z",
        }
    ]
    mock_resp2.headers = {}
    mock_resp2.raise_for_status = MagicMock()

    mock_client = MagicMock()
    mock_client.get = AsyncMock(side_effect=[mock_resp1, mock_resp2])

    # Patch get_github_token so it doesn't shell out
    mocker.patch("radioactive_ralph.forge.github.get_github_token", return_value="fake-token")

    async with GitHubForge(info, token="fake-token", http_client=mock_client) as forge:
        prs = await forge.list_prs()

    assert len(prs) == 2
    assert prs[0].number == 1
    assert prs[1].number == 2
    assert mock_client.get.call_count == 2


@pytest.mark.asyncio
async def test_github_forge_get_ci_integration(mocker) -> None:
    """Test fetching CI status combining check-runs and statuses."""
    info = ForgeInfo(
        host="github.com",
        slug="org/repo",
        forge_type="github",
        api_base_url="https://api.github.com",
    )

    mock_resp_runs = MagicMock()
    mock_resp_runs.json.return_value = {
        "check_runs": [{"name": "test", "status": "completed", "conclusion": "success"}]
    }
    mock_resp_runs.headers = {}
    mock_resp_runs.raise_for_status = MagicMock()

    mock_resp_status = MagicMock()
    mock_resp_status.json.return_value = {
        "statuses": [{"context": "security/audit", "state": "success"}]
    }
    mock_resp_status.raise_for_status = MagicMock()

    mock_client = MagicMock()
    mock_client.get = AsyncMock(side_effect=[mock_resp_runs, mock_resp_status])

    mocker.patch("radioactive_ralph.forge.github.get_github_token", return_value="fake-token")

    pr = ForgePR(
        number=1,
        title="t",
        author="a",
        branch="b",
        head_sha="abc",
        is_draft=False,
        url="",
        updated_at=datetime.now(UTC),
    )

    async with GitHubForge(info, token="fake-token", http_client=mock_client) as forge:
        ci = await forge.get_pr_ci(pr)

    assert ci.state == CIState.SUCCESS
    assert len(ci.details) == 2
