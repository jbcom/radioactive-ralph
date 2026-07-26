#!/usr/bin/env bash
# Fail closed unless the repository's live immutable-release setting is enabled.
# This endpoint requires repository Administration: read, which the built-in
# GitHub Actions token does not provide. The Doppler repository-sync
# CI_GITHUB_TOKEN is used only for this command.
set -euo pipefail

GH_BIN="${GH_BIN:-gh}"
JQ_BIN="${JQ_BIN:-jq}"
CI_GITHUB_TOKEN="${CI_GITHUB_TOKEN:-}"

if [[ -z "$CI_GITHUB_TOKEN" ]]; then
  echo "::error::CI_GITHUB_TOKEN is not configured" >&2
  exit 1
fi

if ! response="$(
  GH_TOKEN="$CI_GITHUB_TOKEN" "$GH_BIN" api \
    -H "X-GitHub-Api-Version: 2026-03-10" \
    "repos/${GITHUB_REPOSITORY}/immutable-releases" 2>/dev/null
)"; then
  echo "::error::immutable-release settings are inaccessible; CI_GITHUB_TOKEN must provide repository Administration read" >&2
  exit 1
fi

if ! enabled="$(
  "$JQ_BIN" -r \
    '.enabled | if type == "boolean" then . else error("missing enabled boolean") end' \
    <<<"$response" 2>/dev/null
)"; then
  echo "::error::immutable-release settings returned an invalid response" >&2
  exit 1
fi

if [[ "$enabled" != "true" ]]; then
  echo "::error::repository immutable releases are disabled" >&2
  exit 1
fi

echo "Repository immutable releases are enabled."
