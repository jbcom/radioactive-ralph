#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
REMOTE="$TMP/remote.git"
WORK="$TMP/work"
BRANCH="chore/update-radioactive-ralph-gui-cask-1.2.3"
REF="refs/heads/$BRANCH"

git init -q --bare "$REMOTE"
git init -q "$WORK"
git -C "$WORK" config user.name test
git -C "$WORK" config user.email test@example.invalid
git -C "$WORK" remote add origin "$REMOTE"

printf 'first\n' > "$WORK/value.txt"
git -C "$WORK" add value.txt
git -C "$WORK" commit -q -m first
(
  cd "$WORK"
  bash "$ROOT/packaging/macos/push-version-branch.sh" "$BRANCH"
)
first_head="$(git -C "$WORK" rev-parse HEAD)"
first_remote="$(git --git-dir="$REMOTE" rev-parse "$REF")"
[[ "$first_remote" == "$first_head" ]]

printf 'second\n' > "$WORK/value.txt"
git -C "$WORK" add value.txt
git -C "$WORK" commit -q -m second
(
  cd "$WORK"
  # The local clone still has no origin/$BRANCH tracking ref. The helper must
  # learn the exact remote SHA and use it as the explicit rerun lease.
  bash "$ROOT/packaging/macos/push-version-branch.sh" "$BRANCH"
)
second_head="$(git -C "$WORK" rev-parse HEAD)"
second_remote="$(git --git-dir="$REMOTE" rev-parse "$REF")"
[[ "$second_remote" == "$second_head" ]]
[[ "$second_remote" != "$first_remote" ]]

echo "version branch lease tests: ok"
