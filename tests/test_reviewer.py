from __future__ import annotations

import json
from datetime import UTC, datetime

import anthropic
import pytest
from pytest_mock import MockerFixture

from radioactive_ralph.forge.base import ForgeClient
from radioactive_ralph.models import PRInfo, PRStatus, ReviewSeverity
from radioactive_ralph.reviewer import (
    batch_review,
    build_review_prompt,
    get_pr_diff,
    parse_review_response,
    review_pr,
)


@pytest.fixture
def mock_pr_info() -> PRInfo:
    return PRInfo(
        repo="org/repo",
        number=1,
        title="Test PR",
        author="alice",
        branch="feat",
        url="http://github.com/org/repo/pull/1",
        status=PRStatus.NEEDS_REVIEW,
        updated_at=datetime.now(UTC),
    )


@pytest.fixture
def mock_forge_client(mocker: MockerFixture) -> ForgeClient:
    return mocker.AsyncMock(spec=ForgeClient)


@pytest.mark.asyncio
async def test_get_pr_diff_success(mock_pr_info, mock_forge_client):
    mock_forge_client.get_pr_diff.return_value = "diff --git a/b b/b"
    diff = await get_pr_diff(mock_pr_info, mock_forge_client)
    assert diff == "diff --git a/b b/b"


@pytest.mark.asyncio
async def test_get_pr_diff_exception(mock_pr_info, mock_forge_client):
    mock_forge_client.get_pr_diff.side_effect = Exception("API error")
    diff = await get_pr_diff(mock_pr_info, mock_forge_client)
    assert diff is None

def test_build_review_prompt(mock_pr_info):
    prompt = build_review_prompt(mock_pr_info, "hello diff")
    assert "PR #1: Test PR" in prompt
    assert "Author: alice" in prompt
    assert "hello diff" in prompt

def test_parse_review_response_success():
    data = {
        "approved": True,
        "summary": "Looks good",
        "findings": [
            {
                "severity": "error",
                "file": "test.py",
                "line": 10,
                "issue": "Bad var",
                "fix": "Fix var"
            }
        ]
    }
    raw = json.dumps(data)
    approved, summary, findings = parse_review_response(raw)
    assert approved is True
    assert summary == "Looks good"
    assert len(findings) == 1
    assert findings[0].severity == ReviewSeverity.ERROR
    assert findings[0].file == "test.py"

def test_parse_review_response_markdown_json():
    data = {"approved": False, "summary": "Fix issues", "findings": []}
    raw = f"Here is my review:\n```json\n{json.dumps(data)}\n```"
    approved, summary, findings = parse_review_response(raw)
    assert approved is False
    assert summary == "Fix issues"

def test_parse_review_response_markdown_plain():
    data = {"approved": True, "summary": "Nice", "findings": []}
    raw = f"Review:\n```\n{json.dumps(data)}\n```"
    approved, summary, findings = parse_review_response(raw)
    assert approved is True
    assert summary == "Nice"

def test_parse_review_response_invalid_json():
    raw = "{ bad json "
    approved, summary, findings = parse_review_response(raw)
    assert approved is False
    assert summary == "Failed to parse review"
    assert findings == []

def test_parse_review_response_invalid_finding_type():
    data = {
        "approved": True,
        "findings": ["this is a string, not a dict"]
    }
    raw = json.dumps(data)
    approved, summary, findings = parse_review_response(raw)
    assert approved is True
    assert len(findings) == 0

def test_parse_review_response_invalid_severity():
    data = {
        "approved": True,
        "findings": [{"severity": "SUPER_ERROR", "file": "test.py"}]
    }
    raw = json.dumps(data)
    approved, summary, findings = parse_review_response(raw)
    assert approved is True
    assert len(findings) == 1
    assert findings[0].severity == ReviewSeverity.SUGGESTION


@pytest.mark.asyncio
async def test_review_pr_success(mock_pr_info, mock_forge_client, mocker):
    mocker.patch("radioactive_ralph.reviewer.get_pr_diff", return_value="diff")
    
    mock_anthropic = mocker.AsyncMock()
    mock_msg = mocker.Mock()
    mock_block = mocker.Mock()
    mock_block.text = json.dumps({"approved": True, "summary": "Good", "findings": []})
    mock_msg.content = [mock_block]
    mock_anthropic.messages.create.return_value = mock_msg
    
    result = await review_pr(mock_pr_info, "/repo", mock_forge_client, client=mock_anthropic)
    assert result.approved is True
    assert result.summary == "Good"


@pytest.mark.asyncio
async def test_review_pr_dict_content(mock_pr_info, mock_forge_client, mocker):
    mocker.patch("radioactive_ralph.reviewer.get_pr_diff", return_value="diff")
    
    mock_anthropic = mocker.AsyncMock()
    mock_msg = mocker.Mock()
    mock_msg.content = [{"text": json.dumps({"approved": True, "summary": "Dict content", "findings": []})}]
    mock_anthropic.messages.create.return_value = mock_msg
    
    result = await review_pr(mock_pr_info, "/repo", mock_forge_client, client=mock_anthropic)
    assert result.approved is True
    assert result.summary == "Dict content"


@pytest.mark.asyncio
async def test_review_pr_no_client(mock_pr_info, mock_forge_client, mocker):
    mocker.patch("radioactive_ralph.reviewer.get_pr_diff", return_value="diff")
    mock_anthropic = mocker.AsyncMock()
    mocker.patch("anthropic.AsyncAnthropic", return_value=mock_anthropic)
    
    mock_msg = mocker.Mock()
    mock_block = mocker.Mock()
    mock_block.text = json.dumps({"approved": True, "summary": "Good", "findings": []})
    mock_msg.content = [mock_block]
    mock_anthropic.messages.create.return_value = mock_msg
    
    result = await review_pr(mock_pr_info, "/repo", mock_forge_client)
    assert result.approved is True


@pytest.mark.asyncio
async def test_review_pr_diff_none(mock_pr_info, mock_forge_client, mocker):
    mocker.patch("radioactive_ralph.reviewer.get_pr_diff", return_value=None)
    result = await review_pr(mock_pr_info, "/repo", mock_forge_client)
    assert result.approved is False
    assert "Failed to fetch PR diff" in result.summary


@pytest.mark.asyncio
async def test_review_pr_diff_empty(mock_pr_info, mock_forge_client, mocker):
    mocker.patch("radioactive_ralph.reviewer.get_pr_diff", return_value="   ")
    result = await review_pr(mock_pr_info, "/repo", mock_forge_client)
    assert result.approved is True
    assert "Empty diff" in result.summary


@pytest.mark.asyncio
async def test_batch_review(mock_pr_info, mock_forge_client, mocker):
    mock_result = mocker.Mock(approved=True, summary="Batch")
    mocker.patch("radioactive_ralph.reviewer.review_pr", return_value=mock_result)
    
    mock_anthropic = mocker.AsyncMock()
    mocker.patch("anthropic.AsyncAnthropic", return_value=mock_anthropic)
    
    prs = [
        (mock_pr_info, "/repo1", mock_forge_client),
        (mock_pr_info, "/repo2", mock_forge_client),
    ]
    
    results = await batch_review(prs)
    assert len(results) == 2
    assert results[0].summary == "Batch"
    assert results[1].summary == "Batch"
