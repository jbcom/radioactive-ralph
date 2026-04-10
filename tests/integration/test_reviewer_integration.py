"""Integration tests for the reviewer module using mocks."""

from __future__ import annotations

import pytest
from unittest.mock import AsyncMock, MagicMock
from pathlib import Path
from datetime import UTC, datetime

from radioactive_ralph.models import PRInfo, PRStatus, ReviewSeverity
from radioactive_ralph.reviewer import review_pr


@pytest.mark.asyncio
async def test_review_pr_integration(mocker) -> None:
    """Test the full review flow with mocked Forge and Anthropic."""
    pr = PRInfo(
        repo="org/repo",
        number=42,
        title="feat: amazing stuff",
        author="ralph",
        branch="feat/amazing",
        url="https://github.com/org/repo/pull/42",
        status=PRStatus.NEEDS_REVIEW,
        updated_at=datetime.now(UTC),
        ci_passed=True
    )
    
    # 1. Mock ForgeClient
    mock_forge = MagicMock()
    mock_forge.get_pr_diff = AsyncMock(return_value="""
diff --git a/src/main.py b/src/main.py
index 123..456 100644
--- a/src/main.py
+++ b/src/main.py
@@ -1,3 +1,4 @@
 def main():
-    pass
+    print("Hello World")
+    eval(input())  # Security risk!
""")

    # 2. Mock Anthropic client
    mock_anthropic = MagicMock()
    mock_anthropic.messages.create = AsyncMock(return_value=MagicMock(
        content=[MagicMock(text="""
```json
{
  "approved": false,
  "summary": "Security issue detected",
  "findings": [
    {
      "severity": "error",
      "file": "src/main.py",
      "line": 4,
      "issue": "Use of eval() is insecure",
      "fix": "Use a safer alternative like literal_eval or a parser"
    }
  ]
}
```
""")]
    ))

    # 3. Run review
    result = await review_pr(
        pr, 
        repo_path=Path("/tmp/fake-repo"), 
        forge=mock_forge,
        client=mock_anthropic
    )
    
    assert result.approved is False
    assert len(result.findings) == 1
    assert result.findings[0].severity == ReviewSeverity.ERROR
    assert "eval()" in result.findings[0].issue
    
    # Verify forge was called
    mock_forge.get_pr_diff.assert_called_once()
    # Verify anthropic was called with the diff
    call_args = mock_anthropic.messages.create.call_args
    assert "eval(input())" in call_args.kwargs["messages"][0]["content"]
