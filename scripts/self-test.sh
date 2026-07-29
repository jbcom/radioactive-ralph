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
if ! out=$("$BIN" plan import "$PLAN" 2>&1); then
  if printf '%s' "$out" | grep -q "already exists"; then
    echo "self-test: plan already imported; reporting on the existing run"
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

# Follow until the plan either completes or goes dead. Both are terminal and
# both are answers -- "dead" is a real result, not a failure of the harness.
for _ in $(seq 1 60); do
  sleep 20
  line=$("$BIN" status 2>/dev/null | grep -E "^  plan prove-the-build-is-sound" || true)
  if [ -n "$line" ]; then
    echo "$line"
    break
  fi
  "$BIN" status 2>/dev/null | grep -E "^  (build|unit|race|lint|e2e|claims) " || true
  echo "---"
done

echo
report
