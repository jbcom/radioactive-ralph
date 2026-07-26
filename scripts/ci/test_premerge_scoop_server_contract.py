#!/usr/bin/env python3
"""Static contract proof for the credential-free premerge Scoop smoke."""

from pathlib import Path


root = Path(__file__).resolve().parents[2]
workflow_path = root / ".github" / "workflows" / "release.yml"
workflow = workflow_path.read_text(encoding="utf-8")
job_start = workflow.index("\n  premerge-package-smoke:\n")
job_end = workflow.index("\n  publish-release:\n", job_start)
job = workflow[job_start:job_end]


def require(fragment: str) -> None:
    if fragment not in job:
        raise AssertionError(f"{workflow_path} must contain {fragment!r}")


def require_order(*fragments: str) -> None:
    positions = [job.index(fragment) for fragment in fragments]
    if positions != sorted(positions):
        raise AssertionError(
            f"{workflow_path} has unsafe Scoop operation order: {fragments!r}"
        )


require(
    '$expectedAssetUrl = "https://github.com/$env:GITHUB_REPOSITORY/'
    'releases/download/$env:RELEASE_TAG/$asset"'
)
require("$originalAssetUrl -ne $expectedAssetUrl")
require("$cachedAssetHash -ne $originalAssetHash")
require("$rewrittenManifest.architecture.'64bit'.hash -ne $originalAssetHash")
require("$credentialNames = @('GH_TOKEN', 'PKGS_GH_TOKEN', 'RELEASE_GH_TOKEN')")
require('throw "$credentialName remained before credential-free Scoop execution"')
require('throw "$credentialName appeared before credential-free Scoop execution"')
require("Start-Process -FilePath python")
require("'--bind', '127.0.0.1'")
require("$response = Invoke-WebRequest -UseBasicParsing -Method Head")
require("$response.Headers.'Content-Length' -eq $expectedLength")
require("for ($attempt = 1; $attempt -le 20; $attempt++)")
require("if (-not $ready)")
require("scoop install")
require("if ($LASTEXITCODE -ne 0)")
require("} finally {")
require("Stop-Process -InputObject $server -Force")
require("$server.WaitForExit(10000)")
require("$server.Dispose()")

require_order(
    "$originalAssetUrl -ne $expectedAssetUrl",
    "$cachedAssetHash -ne $originalAssetHash",
    "$manifest.architecture.'64bit'.url = $localAssetUrl",
)
require_order(
    "Remove-Item \"Env:$credentialName\"",
    'throw "$credentialName remained before credential-free Scoop execution"',
    "Start-Process -FilePath python",
    "for ($attempt = 1; $attempt -le 20; $attempt++)",
    "if (-not $ready)",
    'throw "$credentialName appeared before credential-free Scoop execution"',
    "scoop install",
    "} finally {",
    "Stop-Process -InputObject $server -Force",
)

print("premerge Scoop server contract: PASS")
