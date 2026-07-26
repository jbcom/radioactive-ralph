#!/usr/bin/env bash
# Push HEAD to one exact version branch with an explicit lease derived from the
# remote ref itself. This is safe in shallow clones where origin/$BRANCH was
# never fetched and a bare --force-with-lease would use stale or missing state.

set -euo pipefail

BRANCH="${1:?usage: push-version-branch.sh <branch> [remote]}"
REMOTE="${2:-origin}"
REF="refs/heads/${BRANCH}"

remote_lines="$(git ls-remote --heads "$REMOTE" "$REF")"
remote_count="$(printf '%s\n' "$remote_lines" | awk 'NF {count++} END {print count+0}')"
if [[ "$remote_count" -gt 1 ]]; then
  echo "push-version-branch: ambiguous remote ref $REMOTE/$REF" >&2
  exit 1
fi

if [[ "$remote_count" == "1" ]]; then
  remote_sha="$(printf '%s\n' "$remote_lines" | awk 'NF {print $1}')"
  if [[ ! "$remote_sha" =~ ^[0-9a-f]{40,64}$ ]]; then
    echo "push-version-branch: invalid remote SHA for $REMOTE/$REF" >&2
    exit 1
  fi
  lease="--force-with-lease=${REF}:${remote_sha}"
else
  # An empty expected value asserts that the remote ref must not exist.
  lease="--force-with-lease=${REF}:"
fi

git push "$lease" "$REMOTE" "HEAD:${REF}"
