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
  # Do NOT swallow the query's exit status. An auth failure, rate limit, or
  # network blip makes `gh pr list` return nothing, which is indistinguishable
  # from "every PR merged" — so the previous version announced success and ran
  # the verifier on a lie. A failed query is an ERROR, not an empty result.
  if ! prs=$(gh pr list --state open --json number,title \
    --jq '.[]|select(.title|test("^chore\\(main\\): release")|not)|.number'); then
    echo "round $round: gh pr list FAILED (auth, rate limit, or network) — not " \
      "treating that as an empty queue" >&2
    exit 3
  fi
  if [ -z "$prs" ]; then
    echo "round $round: ALL NON-RELEASE PRs MERGED"
    if ! bash scripts/verify-repo-claims.sh; then
      echo "round $round: queue is empty but the verifier FAILED" >&2
      exit 4
    fi
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

  # Do NOT cancel runs here. An earlier version cancelled every queued CI run
  # except the newest per branch, on the theory that superseded runs hold the
  # scarce macOS slots. Both halves of that theory were wrong, and the evidence
  # is worth keeping because the reasoning is seductive:
  #
  #   * A run's top-level status stays "queued" until its LAST job finishes, so a
  #     run reading "queued" routinely has 19-22 of its 23 jobs already SUCCESS.
  #     Cancelling it discards completed macOS work and forces a full re-run --
  #     the opposite of hygiene. #236 sat one job (Test (macos-latest)) from
  #     complete inside a run whose status was "queued".
  #   * The apparent queued=14/running=1 -> queued=6/running=8 improvement was
  #     not caused by the cancels. Runs finish and start continuously; sampling
  #     before and after any action shows movement. Measured later: queued=7 with
  #     running=4 and 22 jobs executing, zero of them macOS, while ubuntu and
  #     windows flowed freely.
  #
  # macOS is a genuinely constrained pool, but the constraint is upstream and
  # waiting is what drains it. The only honest hygiene is leaving it alone.
  # Cancel a run only when a HUMAN knows a specific push obsoleted it.
  sleep 90
done
echo "round cap reached; still open: $(gh pr list --state open --json number --jq 'length')"
