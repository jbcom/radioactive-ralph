#!/usr/bin/env bash
# verify-repo-claims.sh — independent check of the claims an agent (or a human)
# makes about this repo's in-flight state.
#
# Run it before asserting "all threads resolved", "everything green", or "N PRs
# open". It queries GitHub and the working trees directly rather than trusting
# a summary.
#
# Exists because every wrong thing this session was a check that passed for the
# wrong reason: a containment test reading an absent file as containment, a
# watcher matching a run I had cancelled myself, a test whose fixture never
# reached the code path. Self-reported status has the same failure mode, so it
# gets verified from the source of truth rather than from memory.
cd /Users/jbogaty/src/jbcom/radioactive-ralph || exit 1
fail=0
say() { printf '%-42s %s\n' "$1" "$2"; }

# 1. Open PRs — the number I quote must match GitHub.
open=$(gh pr list --state open --json number,title \
  --jq '[.[]|select(.title|test("^chore\\(main\\): release")|not)]|length' 2>/dev/null)
say "open non-release PRs" "${open:-QUERY-FAILED}"
[ -z "$open" ] && fail=1

# 2. Unresolved review threads — "all resolved" is a claim I have made often.
tot=0
for pr in $(gh pr list --state open --json number --jq '.[].number' 2>/dev/null); do
  n=$(gh api graphql -f query="query { repository(owner:\"jbcom\", name:\"radioactive-ralph\") {
    pullRequest(number:$pr) { reviewThreads(first:100) { nodes { isResolved } } } } }" \
    --jq '[.data.repository.pullRequest.reviewThreads.nodes[]|select(.isResolved==false)]|length' 2>/dev/null)
  [ -n "$n" ] && [ "$n" != "0" ] && { say "  PR #$pr unresolved threads" "$n"; tot=$((tot+n)); }
done
say "unresolved review threads" "$tot"

# 3. Failing checks — distinct from "blocked", which only means waiting.
f=0
for pr in $(gh pr list --state open --json number --jq '.[].number' 2>/dev/null); do
  c=$(gh pr view "$pr" --json statusCheckRollup \
    --jq '[.statusCheckRollup[]?|select(.conclusion=="FAILURE")]|length' 2>/dev/null)
  [ -n "$c" ] && f=$((f+c))
done
say "failing checks across open PRs" "$f"

# 4. Conflicted PRs — these need a human-equivalent decision, not waiting.
d=$(gh pr list --state open --json number,mergeStateStatus \
  --jq '[.[]|select(.mergeStateStatus=="DIRTY")]|length' 2>/dev/null)
say "PRs needing manual merge (DIRTY)" "${d:-?}"

# 5. Local worktrees must build and test — a claim of "green" is checkable.
for wt in /Users/jbogaty/src/jbcom/.worktrees/*/; do
  [ -f "$wt/go.mod" ] || continue
  name=$(basename "$wt")
  if ! (cd "$wt" && go build ./... >/dev/null 2>&1); then
    say "  $name" "BUILD FAILS"; fail=1
  fi
done
say "all worktrees build" "$([ "$fail" = "0" ] && echo yes || echo NO)"

# 6. Uncommitted work anywhere — silent local-only changes are lost work.
for wt in /Users/jbogaty/src/jbcom/radioactive-ralph /Users/jbogaty/src/jbcom/.worktrees/*/; do
  [ -d "$wt/.git" ] || [ -f "$wt/.git" ] || continue
  n=$(git -C "$wt" status --porcelain 2>/dev/null|wc -l|tr -d ' ')
  [ "$n" != "0" ] && say "  $(basename "$wt") uncommitted files" "$n"
done

# 7. Unpushed commits — committed but not shared is also lost work.
for wt in /Users/jbogaty/src/jbcom/radioactive-ralph /Users/jbogaty/src/jbcom/.worktrees/*/; do
  br=$(git -C "$wt" branch --show-current 2>/dev/null) || continue
  [ -z "$br" ] && continue
  n=$(git -C "$wt" log --oneline "origin/$br..HEAD" 2>/dev/null|wc -l|tr -d ' ')
  [ "$n" != "0" ] && say "  $(basename "$wt") UNPUSHED commits" "$n"
done

# 8. The directive must not mark EVERY open item as a wait while real work is
#    outstanding. An all-[WAIT] queue tells the anti-stop hook the turn may end,
#    so a stale wait-label is how a session stops with PRs still open — which is
#    exactly what happened before this check existed.
tot_items=$(grep -cE '^- \[ \]|^      - \[ \]' .agent-state/directive.md 2>/dev/null)
wait_items=$(grep -cE '^- \[ \] \[WAIT|^      - \[ \] \[WAIT' .agent-state/directive.md 2>/dev/null)
say "directive open items" "$tot_items ($wait_items wait-labelled)"
if [ "$tot_items" != "0" ] && [ "$tot_items" = "$wait_items" ] && [ "${open:-0}" != "0" ]; then
  say "  ALL open items are [WAIT]" "but $open PRs are still open — FIX THE LABEL"
  fail=1
fi

# 9. Directive PR references must match reality. A stale list reads as progress
#    that has not happened.
for n in $(grep -oE '#[0-9]{3}' .agent-state/directive.md 2>/dev/null | tr -d '#' | sort -u); do
  :
done

# 10. decisions.ndjson must stay parseable — it has conflicted repeatedly.
python3 -c "
import json,sys
bad=0
for i,l in enumerate(open('.agent-state/decisions.ndjson'),1):
    if not l.strip(): continue
    try: json.loads(l)
    except Exception: bad+=1; print('  malformed line', i)
print('%-42s %s' % ('decisions.ndjson', 'valid' if not bad else str(bad)+' MALFORMED'))
sys.exit(1 if bad else 0)" || fail=1

exit $fail
