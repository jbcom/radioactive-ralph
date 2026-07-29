#!/usr/bin/env bash
# Exercise scripts/self-test.sh's tracked-edit guard.
#
# The guard warns when a self-test run modifies tracked source, which happens
# because a provider turn will edit code to make its acceptance command pass.
# One real run rewrote four files across internal/agent, internal/orch and
# internal/provider; the tree still built, and those edits sat in the working
# tree waiting for someone's `git add -A`.
#
# It gets its own test because THREE separate bugs shipped in it, each found by
# exercising it rather than re-reading it:
#
#   1. it referenced $REPO_ROOT, a variable self-test.sh never defines (borrowed
#      from verify-repo-claims.sh) -- it would have crashed on every run.
#   2. the first manual check captured its baseline AFTER staging, so a broken
#      test looked like a broken guard.
#   3. comparing `git status` output was blind to a file that was ALREADY dirty:
#      " M path" before and after while the content changed underneath -- and a
#      file someone is actively editing is exactly the one a concurrent run is
#      most likely to touch.
#
# A guard is code. It needs the same negative proof as the thing it guards.
#
# TRACKED_BEFORE and TRACKED_PATHS_BEFORE are read by report_tracked_edits,
# which is eval'd in from self-test.sh -- shellcheck cannot see through an eval,
# so it reports them unused. File-scope disable: a per-line one would have to be
# repeated at each of the three assertions.
# shellcheck disable=SC2034
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# A scratch repo, so the assertions can dirty tracked files without touching
# the real checkout -- which is the very hazard this guard exists to report.
REPO="$TMP/repo"
mkdir -p "$REPO"
git -C "$REPO" init -q
git -C "$REPO" config user.email test@example.com
git -C "$REPO" config user.name test
printf 'original\n' > "$REPO/tracked.txt"
git -C "$REPO" add tracked.txt
git -C "$REPO" commit -qm init

# Load the guard functions from the real script rather than restating them: a
# copied assertion would keep passing after the original changed.
eval "$(sed -n '/^snapshot_tracked()/,/^}/p;/^tracked_paths()/,/^}/p;/^report_tracked_edits()/,/^}/p' \
  "$ROOT/scripts/self-test.sh")"

cd "$REPO"

fail() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }

# 1. A clean tree must stay silent. A guard that cries wolf on every run is one
#    people learn to ignore, which is the same as not having it.
TRACKED_BEFORE=$(snapshot_tracked)
TRACKED_PATHS_BEFORE=$(tracked_paths)
out=$(report_tracked_edits)
[ -z "$out" ] || fail "warned about an unmodified tree: $out"

# 2. A file modified DURING the run must be named.
TRACKED_BEFORE=$(snapshot_tracked)
TRACKED_PATHS_BEFORE=$(tracked_paths)
printf 'worker edit\n' >> tracked.txt
out=$(report_tracked_edits)
printf '%s' "$out" | grep -q 'tracked.txt' ||
  fail "did not name a file the run modified: $out"

# 3. THE REGRESSION: a file already dirty when the run starts, then rewritten.
#    `git status` reports " M tracked.txt" both before and after, so a
#    status-based comparison sees nothing while the content changed.
git checkout -- tracked.txt
printf 'pre-existing edit\n' >> tracked.txt   # dirty BEFORE the run
TRACKED_BEFORE=$(snapshot_tracked)
TRACKED_PATHS_BEFORE=$(tracked_paths)
printf 'worker rewrite\n' >> tracked.txt      # rewritten DURING the run
out=$(report_tracked_edits)
printf '%s' "$out" | grep -q 'tracked.txt' ||
  fail "blind to a rewrite of an already-dirty file -- the case the guard exists for: $out"

printf 'self-test tracked-edit guard: 3 checks passed\n'
