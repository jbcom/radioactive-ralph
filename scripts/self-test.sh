#!/usr/bin/env bash
# self-test.sh — have Ralph verify Ralph, using Ralph.
#
# This is the dogfooding entry point. It imports docs/plans/self-test.md into a
# running supervisor and reports what the operator surface says about it, so the
# product's own observability is what tells you whether the product works.
#
# WHY A SCRIPT AND NOT A README STEP
#
# The first dogfooding attempt failed twice for reasons that had nothing to do
# with the code under test:
#
#   1. The plan lived in .radioactive-ralph/plans/, which is gitignored (the
#      product contract forbids committed repo state). Switching branches
#      deleted it. A self-test you have to re-author is one nobody runs twice.
#      The plan now lives in docs/plans/ as a tracked SOURCE file; the state it
#      produces still goes to the user-level DB, so the contract holds.
#
#   2. No step carried an `accept:` marker, so every task was judgment-only --
#      accepted on non-empty worker evidence, failed on empty. Nothing was ever
#      actually verified. Every step in the plan now carries an inline
#      `accept:` command that the orchestrator re-runs itself.
#
# USAGE
#
#   scripts/self-test.sh            # import and report once
#   scripts/self-test.sh --watch    # import and follow until it settles
#
# A supervisor must already be running (radioactive_ralph --supervisor).
set -euo pipefail

cd "$(dirname "$0")/.."
PLAN="docs/plans/self-test.md"
BIN="${RALPH_BIN:-./radioactive_ralph}"

[ -f "$PLAN" ] || { echo "self-test: missing $PLAN" >&2; exit 1; }

if [ ! -x "$BIN" ]; then
  echo "self-test: building $BIN"
  go build -o "$BIN" ./cmd/radioactive_ralph
fi

# Import fails loudly if no supervisor is listening, which is the correct
# behaviour: a self-test that silently skips when the product is not running
# would report success for a system that never started.
#
# A re-import of the same slug is REFUSED (fail-closed on conflict), which is
# right for the store but makes a self-test un-rerunnable -- and a check you can
# only run once is one nobody runs. Treat that specific conflict as "already
# imported, report on the existing run" rather than an error, while any OTHER
# import failure still stops the script.
# Each run imports under a UNIQUE title, hence a unique slug.
#
# The first version treated a duplicate-slug conflict as "already imported" and
# reported on the existing run. That was wrong, and the review that flagged it
# was right: the stored plan keeps whatever steps it had at first import, so a
# re-run after changing the plan OR the code reports a STALE result. Verified
# after this landed -- the stored plan still had 6 tasks while the file defined
# 12, and none of the new step ids existed. A self-test that reports green for
# code it never ran is the worst possible outcome for this script.
#
# The tracked plan is never modified: only the H1 title is rewritten, into a
# temp copy. Old runs stay in the DB as history, and the operator surface marks
# them "no runnable work" so a stale plan is visibly distinct from a live one.
# Second-resolution alone collides: two invocations in the same second derive
# the same title, hence the same slug, and the store's per-project uniqueness
# constraint rejects the second import. mktemp's token is the collision-
# resistant part and costs nothing, since the temp file is created anyway.
TMP_PLAN="$(mktemp -t ralph-self-test-XXXXXX).md"
# LOWERCASED, because plan.Slug lowercases the title when deriving the slug.
# mktemp's token is mixed case, so a mixed-case RUN_ID produced a RUN_SLUG that
# could never equal the stored slug -- the exact-match watch would have silently
# never fired, reintroducing the stale-run bug in a new disguise.
RUN_ID="$(date -u +%Y%m%d-%H%M%S)-$(basename "$TMP_PLAN" .md | sed 's/^ralph-self-test-//' | tr '[:upper:]' '[:lower:]')"
sed "1s|^# .*|# Prove the build is sound ($RUN_ID)|" "$PLAN" > "$TMP_PLAN"

# Snapshot the tracked tree BEFORE the run.
#
# A self-test step can edit tracked source: a provider turn trying to make its
# acceptance command pass will change code to do it. One run rewrote four files
# across internal/agent, internal/orch and internal/provider, adding an
# integer-conversion guard for a lint error that -- checked afterwards -- did
# not actually reproduce. The tree still built, so nothing objected, and those
# edits sat in the working tree waiting for someone's `git add -A`.
#
# That is the agent working as designed, not a malfunction. But it means a run
# leaves changes nobody authored, and the only thing standing between them and
# a commit was whoever happened to read `git status`. Report them instead.
# CONTENT hashes, not `git status` flags. A path already dirty when the run
# starts reports " M path" both before and after, so a worker rewriting that
# same file is invisible to a status comparison -- and a file someone is
# actively editing is exactly the one a concurrent run is most likely to touch.
# `git diff` hashes the working tree, so any change to content shows up.
snapshot_tracked() {
  # Against HEAD, so STAGED changes count too. Plain `git diff` shows only the
  # unstaged half: once a worker's edit is staged the hashes match again and the
  # guard goes quiet -- on the state that is one keystroke from committed, which
  # is the worst moment to fall silent.
  git diff --no-color HEAD 2>/dev/null | git hash-object --stdin 2>/dev/null || true
}
tracked_paths() {
  git diff --name-only HEAD 2>/dev/null || true
}
TRACKED_BEFORE=$(snapshot_tracked)
TRACKED_PATHS_BEFORE=$(tracked_paths)

# report_tracked_edits names any TRACKED file the run changed. Untracked scratch
# is expected and gitignored; a modified source file is not, and it is the thing
# a reader is least likely to notice on their own.
report_tracked_edits() {
  local after paths
  after=$(snapshot_tracked)
  [ "$after" = "$TRACKED_BEFORE" ] && return 0
  echo
  echo "self-test: WARNING — this run modified tracked files:"
  # Every path dirty now that was not dirty before, PLUS any path that was
  # already dirty (its content changed, or the hash above would have matched).
  paths=$(tracked_paths)
  {
    comm -13 <(printf '%s\n' "$TRACKED_PATHS_BEFORE" | sort) \
             <(printf '%s\n' "$paths" | sort)
    printf '%s\n' "$TRACKED_PATHS_BEFORE" | sort
  } | sort -u | sed '/^$/d;s/^/  /'
  echo "  A provider turn edits source to make its acceptance pass. Review these"
  echo "  before committing -- you did not write them."
}

# Report tracked edits on EVERY exit, not just the success path.
#
# `set -e` plus a failing import (no supervisor, a bad plan, a slug conflict)
# exits before report() is ever reached -- and a run that died partway is
# exactly the one likely to leave a half-finished worker edit behind, with
# nobody told about it. Verified: an aborted run printed no warning at all
# while a modified tracked file sat in the tree.
#
# The EXIT trap already removes the temp plan; chaining keeps both.
trap 'rm -f "$TMP_PLAN"; report_tracked_edits' EXIT

echo "self-test: importing $PLAN as run $RUN_ID"
"$BIN" plan import "$TMP_PLAN"

report() {
  # Same page bound the watch loop uses. Each run adds a plan, so on the default
  # 50-plan page the FINAL report is the first thing to stop showing the run you
  # just started -- silently, since a short page looks identical to a small
  # project. Accumulation is allowed to become noise; it must not become a
  # report that omits its own subject.
  "$BIN" status --plan-limit 200 --task-limit 200

  # The same hazard reaches TASKS, and the comment above only solved it for
  # plans. MaxOperatorPageLimit is 200 for both, and every run adds 12 tasks --
  # so after ~16 runs the page saturates and the newest run is shown PARTIALLY.
  # Observed: 200 rows across 19 plans, with the current run contributing 6 of
  # its 12 tasks. has_more says so; nothing was reading it.
  if "$BIN" status --json --plan-limit 200 --task-limit 200 2>/dev/null |
     python3 -c 'import json,sys
try: d=json.load(sys.stdin)
except Exception: sys.exit(1)
sys.exit(0 if d.get("tasks",{}).get("has_more") else 1)' 2>/dev/null; then
    echo
    echo "self-test: WARNING — the task page is FULL (has_more), so the rows above"
    echo "  are a partial view and may omit this run's own tasks. Prune old plans"
    echo "  (radioactive_ralph plan delete) or read this run by plan id."
  fi
}


if [ "${1:-}" != "--watch" ]; then
  echo
  report
  exit 0
fi

# Follow until the plan reaches a TERMINAL state, which is either outcome:
# every step done, or the plan dead because a step failed.
#
# The first version broke as soon as the plan's warning line appeared -- but
# writePlanWarnings emits that line ONLY when the plan is dead, so a successful
# run never matched and sat through every poll before reporting. Watching for
# one outcome and calling it "terminal" is how a watcher hangs on success.
missing=0
for _ in $(seq 1 90); do
  sleep 20
  # Raise the plan page well past the default 50. This script adds one plan per
  # run, so a project that has self-tested 50 times would stop seeing the newest
  # one -- a failure the script inflicts on itself, presenting as a silent
  # timeout rather than an error. MaxOperatorPageLimit is 200; the loop also
  # fails loudly below if the run is still not found.
  snapshot=$("$BIN" status --json --plan-limit 200 2>/dev/null) || continue
  state=$(printf '%s' "$snapshot" | RUN_SLUG="prove-the-build-is-sound-$RUN_ID" python3 -c '
import json,os,sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit()
for p in d.get("plans", {}).get("items", []):
    if p.get("slug") == os.environ.get("RUN_SLUG"):
        done, total = p.get("task_done", 0), p.get("task_total", 0)
        if p.get("no_runnable_work"):
            print(f"DEAD {done}/{total}")
        elif total and done >= total:
            print(f"COMPLETE {done}/{total}")
        else:
            print(f"RUNNING {done}/{total}")
' 2>/dev/null)
  if [ -z "$state" ]; then
    # The run is not in the page at all. Silence here would look exactly like a
    # slow run, so say so instead of polling on in the dark.
    missing=$((missing + 1))
    if [ "$missing" -ge 3 ]; then
      echo "self-test: run $RUN_ID not found in the snapshot after $missing polls" >&2
      echo "self-test: too many stored plans for one page? try pruning old runs" >&2
      break
    fi
    continue
  fi
  missing=0
  echo "self-test: $state"
  case "$state" in
    COMPLETE*) echo "self-test: every step verified"; break ;;
    DEAD*)     echo "self-test: a step failed terminally — see the rows below"; break ;;
  esac
done

echo
report
