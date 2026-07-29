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
echo "self-test: importing $PLAN"
# Match THIS plan's slug, not any "already exists" text. A bare substring match
# swallowed unrelated conflicts (a duplicate task id, a colliding reservation)
# and reported them as a completed import -- the failure mode this whole session
# has been about: an error path whose output is indistinguishable from success.
SLUG="prove-the-build-is-sound"
if ! out=$("$BIN" plan import "$PLAN" 2>&1); then
  if printf '%s' "$out" | grep -q "plan with slug already exists" &&
     printf '%s' "$out" | grep -q "$SLUG"; then
    echo "self-test: $SLUG already imported; reporting on the existing run"
  else
    printf '%s\n' "$out" >&2
    exit 1
  fi
else
  printf '%s\n' "$out"
fi

report() {
  "$BIN" status
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
for _ in $(seq 1 90); do
  sleep 20
  snapshot=$("$BIN" status --json 2>/dev/null) || continue
  state=$(printf '%s' "$snapshot" | python3 -c '
import json,sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit()
for p in d.get("plans", {}).get("items", []):
    if p.get("slug") == "prove-the-build-is-sound":
        done, total = p.get("task_done", 0), p.get("task_total", 0)
        if p.get("no_runnable_work"):
            print(f"DEAD {done}/{total}")
        elif total and done >= total:
            print(f"COMPLETE {done}/{total}")
        else:
            print(f"RUNNING {done}/{total}")
' 2>/dev/null)
  [ -z "$state" ] && continue
  echo "self-test: $state"
  case "$state" in
    COMPLETE*) echo "self-test: every step verified"; break ;;
    DEAD*)     echo "self-test: a step failed terminally — see the rows below"; break ;;
  esac
done

echo
report
