#!/usr/bin/env bash
# Fail when a repo shell script is missing from ci.yml's shellcheck allowlist.
#
# The list is explicit rather than a glob, so a NEW script is silently exempt
# until someone remembers to add it. Seven scripts had accumulated that way,
# including the two that back the claim-verification workflow — an unchecked
# script is indistinguishable from a checked one until it breaks.
#
# A glob in ci.yml would be simpler but loses the ability to exclude a
# deliberately-unchecked file; this keeps the explicit list AND notices omissions.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

listed="$(sed -n '/shellcheck -x \\/,/^$/p' .github/workflows/ci.yml \
  | grep -oE '[a-z0-9/_.-]+\.sh' | sort -u)"
# git ls-files, not `find scripts packaging docs`: a hardcoded directory list
# makes tracked scripts elsewhere invisible to this gate, which is the same
# blind spot one level up. Three were hiding — .claude/hooks/task-batch-flush.sh
# and two under reference/.
#
# EXCLUSIONS are explicit and justified, not incidental:
#   reference/  — vendored prior art, not ours to fix.
present="$(git ls-files '*.sh' | grep -vE '^reference/' | sort -u)"

missing="$(comm -13 <(echo "$listed") <(echo "$present") || true)"
if [[ -n "$missing" ]]; then
  echo "shell scripts absent from ci.yml's shellcheck list:" >&2
  echo "$missing" >&2
  echo >&2
  echo "Add each to the 'Shellcheck packaging + install scripts' step, or" >&2
  echo "delete it. An unchecked script looks identical to a checked one." >&2
  exit 1
fi

stale="$(comm -23 <(echo "$listed") <(echo "$present") || true)"
if [[ -n "$stale" ]]; then
  echo "ci.yml shellchecks files that no longer exist:" >&2
  echo "$stale" >&2
  exit 1
fi

echo "shellcheck coverage: $(echo "$present" | wc -l | tr -d ' ') scripts, all listed"
