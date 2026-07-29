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

# 4. A STAGED edit. Plain `git diff` shows only unstaged changes, so once a
#    worker's edit is staged the snapshots match again and the guard falls
#    silent -- on the state that is one keystroke from committed. Every other
#    check here leaves its mutation unstaged, so none of them would catch it.
git checkout -- tracked.txt
git reset -q
TRACKED_BEFORE=$(snapshot_tracked)
TRACKED_PATHS_BEFORE=$(tracked_paths)
printf 'worker edit then staged\n' >> tracked.txt
git add tracked.txt
out=$(report_tracked_edits)
printf '%s' "$out" | grep -q 'tracked.txt' ||
  fail "blind to a STAGED edit -- the state closest to being committed: $out"

# 5. The FAILURE path. `set -e` plus a failing import exits before report() is
#    ever reached, so the guard only ever ran on success -- and a run that died
#    partway is exactly the one likely to leave a half-finished worker edit
#    behind. The guard now runs from an EXIT trap; this proves the trap fires
#    and that report_tracked_edits is defined before it is installed (it was
#    not, at first: the trap referenced a function declared 18 lines later and
#    silently did nothing).
cd "$ROOT"
FAKE_BIN="$(mktemp -t fakeralph-XXXXXX)"
cat > "$FAKE_BIN" <<'FAKE'
#!/bin/sh
case "$1" in
  plan) printf '\n// edit by fake worker\n' >> "$SELFTEST_VICTIM"; exit 1 ;;
  *) exit 0 ;;
esac
FAKE
chmod +x "$FAKE_BIN"
# The victim must be a file git TRACKS in this repo, or there is nothing for the
# guard to notice. Use the plan the self-test itself imports: appending a
# markdown comment cannot change its meaning, and it is restored immediately.
VICTIM="docs/plans/self-test.md"
out=$(SELFTEST_VICTIM="$VICTIM" RALPH_BIN="$FAKE_BIN" bash "$ROOT/scripts/self-test.sh" 2>&1 || true)
git -C "$ROOT" checkout -- "$VICTIM" 2>/dev/null || true
rm -f "$FAKE_BIN"
printf '%s' "$out" | grep -q 'WARNING' ||
  fail "a run that FAILED at import printed no tracked-edit warning: $out"

printf 'self-test tracked-edit guard: 5 checks passed\n'
