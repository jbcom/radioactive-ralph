#!/usr/bin/env bash
# drive-open-prs.sh — drive every open non-release PR to merged.
#
# Rebases BEHIND, arms auto-merge, merges CLEAN/UNSTABLE with no failures, and
# reports blocked/behind/ready/DIRTY/FAILING on every state change. Exits 2 the
# moment something needs a decision.
#
# The reporting is the point. An earlier version printed only an open-PR count,
# which looks identical whether CI is merely slow, a PR has a conflict, or tests
# are failing — so a stall was indistinguishable from patience. This version
# surfaced a real #245 failure within two rounds.
cd /Users/jbogaty/src/jbcom/radioactive-ralph || exit 1
prev=""
for round in $(seq 1 400); do
  prs=$(gh pr list --state open --json number,title \
    --jq '.[]|select(.title|test("^chore\\(main\\): release")|not)|.number' 2>/dev/null)
  if [ -z "$prs" ]; then
    echo "round $round: ALL NON-RELEASE PRs MERGED"
    bash scripts/verify-repo-claims.sh
    exit 0
  fi
  blocked=0; behind=0; ready=0; dirty=""; failing=""; merged=""
  for pr in $prs; do
    st=$(gh pr view "$pr" --json mergeStateStatus --jq '.mergeStateStatus' 2>/dev/null)
    f=$(gh pr view "$pr" --json statusCheckRollup \
      --jq '[.statusCheckRollup[]?|select(.conclusion=="FAILURE")|.name]|join(",")' 2>/dev/null)
    [ -n "$f" ] && failing="$failing $pr($f)"
    case "$st" in
      BEHIND) behind=$((behind+1)); gh pr update-branch "$pr" >/dev/null 2>&1 ;;
      DIRTY)  dirty="$dirty $pr" ;;
      CLEAN|UNSTABLE)
        if [ -z "$f" ]; then
          ready=$((ready+1))
          gh pr merge "$pr" --squash --delete-branch >/dev/null 2>&1 && merged="$merged $pr"
        fi ;;
      *) blocked=$((blocked+1)) ;;
    esac
    armed=$(gh pr view "$pr" --json autoMergeRequest --jq 'if .autoMergeRequest then 1 else 0 end' 2>/dev/null)
    [ "$armed" = "0" ] && gh pr merge "$pr" --squash --auto --delete-branch >/dev/null 2>&1
  done
  n=$(echo "$prs"|wc -w|tr -d ' ')
  line="round $round: open=$n blocked=$blocked behind=$behind ready=$ready"
  [ -n "$merged" ]  && line="$line MERGED:$merged"
  [ -n "$dirty" ]   && line="$line DIRTY:$dirty"
  [ -n "$failing" ] && line="$line FAILING:$failing"
  [ "$line" != "$prev" ] && { echo "$line"; prev="$line"; }
  { [ -n "$dirty" ] || [ -n "$failing" ]; } && { echo "STOP: needs a decision"; exit 2; }
  sleep 90
done
echo "round cap reached; still open: $(gh pr list --state open --json number --jq 'length')"
