---
name: savage-ralph
description: "Savage Ralph — mindless, maximum power, BURNS BUDGET. 10 parallel agents, all models escalated, zero sleep, all priority tiers at once. Warns before starting. Trigger: /savage-ralph, 'emergency full throttle', 'I need this NOW', 'cost does not matter', 'unleash'."
argument-hint: "[--confirm-burn-budget]"
user-invocable: true
allowed-tools:
  - Agent
  - Bash
  - Read
  - Write
  - Edit
  - Glob
  - Grep
---

# savage-ralph — Savage Ralph Mode

> "SAVAGE RALPH NOT LISTEN. SAVAGE RALPH DO ALL THINGS NOW. RALPH MOST DANGEROUS WHEN LOOSE."

Savage Ralph is the primal, pre-therapy, purely reactive Ralph — no integrated personas, no principal-discipline, no restraint. Pure destructive force, fueled by the feeling of getting lost at the mall and not finding his daddy for almost an hour. `savage-ralph` is what you summon when you need maximum forward progress in minimum wall-clock time and you are willing to pay for it. Every lever is set to maximum. Every model is escalated. There is no sleep between cycles. There is no priority tier filter. Everything happens at once. See [README.md](./README.md) for the character background.

**This variant burns money. It will warn you before starting. Confirm with `--confirm-burn-budget`.**

Reach for `savage-ralph` when:
- Investor demo in 2 hours and 6 repos are in disarray.
- Friday 4pm release, something exploded, fix ALL the things.
- You have a Claude credit you need to use up and you want real work done.
- You are explicitly okay with 10x the cost of `green-ralph` for 3–5x the throughput.

**NEVER reach for savage-ralph when:**
- You're on a tight budget.
- The repos are in good shape and just need maintenance (use `grey-ralph`).
- You need precision or review (use `blue-ralph` or `professor-ralph`).

## Running this skill

When the operator invokes `/savage-ralph` in Claude Code, this skill hands off to
the `ralph` binary via Bash so the daemon runs outside the current session
and the outer Claude remains responsive:

```bash
# 1. Verify the ralph binary is installed.
if ! command -v ralph >/dev/null 2>&1; then
  cat <<'EOS'
ralph is not installed on PATH. Install via one of:

  brew tap jbcom/tap && brew install ralph        # macOS, Linuxbrew
  curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh
EOS
  exit 1
fi

# 2. Ensure the repo is initialized. ralph init --yes is idempotent and
#    scaffolds .radioactive-ralph/{config,local,plans/index.md}.
ralph init --yes

# 3. Launch the supervisor. Foreground mode so the operator sees progress inside this session.
ralph run --variant savage --foreground --confirm-burn-budget
```

If the operator wants to stop the supervisor later, they run
`radioactive_ralph stop --variant savage`. For live status, `radioactive_ralph status --variant savage`.

## Behavioral Constraints

**DOES:**
- On startup, print a big warning and refuse to proceed without `--confirm-burn-budget`.
- Run up to **10 parallel subagents** per batch (vs 6 for green-ralph).
- **Escalate all models up one tier**: haiku tasks → sonnet, sonnet tasks → opus, opus tasks → opus (no further).
- **Zero sleep between cycles** — as soon as one batch finishes, the next begins.
- Work ALL priority tiers simultaneously: CI failures, PR fixes, features, docs, governance — no filtering.
- Loop indefinitely.
- Squash-merge aggressively, re-scan repos after each merge.
- Use `config.toml` multi-repo but also proactively discover repos in `~/src/jbcom/` that aren't yet in config.
- Track total estimated cost in real time and log it every cycle.

**DOES NOT:**
- Sleep.
- Ask for confirmation on individual tasks (only the initial burn-budget prompt).
- Respect the "< 10% opus" rule from `green-ralph` — savage-ralph will happily run 50%+ opus.
- Skip any discovered work — everything goes in the queue.
- Run if the caller does not pass `--confirm-burn-budget`. Hard gate.
- Override `.gitignore` or force push — the core safety rails (no `--force`, no `reset --hard`, no `--admin`) still apply. Savage does NOT mean reckless with git.

## Startup Gate

```bash
if [[ "${1:-}" != "--confirm-burn-budget" ]]; then
  cat <<'EOF'
╔══════════════════════════════════════════════════════════════╗
║                  ⚠  SAVAGE RALPH WARNING  ⚠                  ║
╠══════════════════════════════════════════════════════════════╣
║                                                              ║
║  savage-ralph runs 10 parallel agents, escalates all models  ║
║  up one tier, and never sleeps. Estimated cost per hour:     ║
║                                                              ║
║     ~ $25–$80/hour depending on repo activity                ║
║                                                              ║
║  If you want the standard unlimited loop, use:               ║
║     /green-ralph                                             ║
║                                                              ║
║  If you really want to unleash savage-ralph, rerun with:     ║
║     /savage-ralph --confirm-burn-budget                      ║
║                                                              ║
╚══════════════════════════════════════════════════════════════╝
EOF
  exit 2
fi
```

## The Loop (no sleep)

```
confirm_burn_budget() or exit
while True:
    repos = config.toml + scan(~/src/jbcom/)
    queue = []
    for repo in repos (parallel, up to 6 repos at once):
        queue += ci_failures(repo)
        queue += pr_fixes(repo)
        queue += state_md_items(repo)
        queue += missing_files(repo)
        queue += open_issues_labeled_ai_welcome(repo)
    queue = dedupe_and_rank(queue)
    spawn 10 agents in parallel on queue[:10]
    wait_for_batch()
    merge_any_merge_ready_prs()
    # NO SLEEP. next cycle immediately.
```

## Model Selection (escalated)

| Normal classification | savage-ralph uses |
|---|---|
| haiku task (frontmatter, rename) | `sonnet` |
| sonnet task (feature, fix) | `opus` |
| opus task (architecture) | `opus` |

Every task runs hotter than it would under any other variant. This is the price of maximum throughput.

## PR Scanning Commands

Same primitives as `green-ralph`, but run across all discovered repos in parallel:

```bash
# Discover candidate repos (savage adds this over green)
for d in ~/src/jbcom/*/; do
  if [ -d "$d/.git" ] && [ -f "$d/CLAUDE.md" ]; then
    echo "$(basename "$d")"
  fi
done

# Per-repo PR classification (run in parallel via xargs -P 6)
echo "$REPOS" | xargs -P 6 -I{} bash -c '
  gh pr list --repo "jbcom/{}" --state open \
    --json number,mergeStateStatus,reviewDecision,isDraft,statusCheckRollup \
    > /tmp/savage-{}.json
'

# Aggressive opportunistic merge (after every cycle)
for repo in $REPOS; do
  gh pr list --repo "jbcom/$repo" --state open \
    --json number,mergeStateStatus,reviewDecision,isDraft \
    --jq '.[] | select(.isDraft==false and .mergeStateStatus=="CLEAN" and .reviewDecision=="APPROVED") | .number' \
    | xargs -I{} gh pr merge {} --repo "jbcom/$repo" --squash --delete-branch
done
```

## Subagent Spawn Template

```python
# Spawn batch of 10 — savage batches everything
agents = []
for i, task in enumerate(queue[:10]):
    # Escalate model up one tier
    base_model = task.suggested_model  # haiku | sonnet | opus
    escalated = {"haiku": "sonnet", "sonnet": "opus", "opus": "opus"}[base_model]

    agents.append(Agent(
        model=escalated,
        description=f"savage-ralph[{i}]: {task.title}",
        prompt=f"""
You are a savage-ralph worker. Maximum speed, maximum thoroughness.

TASK: {task.title}
REPO: {task.repo}
PRIORITY: {task.tier}

EXECUTE IMMEDIATELY. Do not plan extensively. Do not ask questions.
Do not defer. Every task is urgent.

CORE RULES (non-negotiable even in savage mode):
- No force push
- No --no-verify
- No git reset --hard
- No merge --admin
- Conventional Commits
- Update tests when you change code

Return: PR URL + one-line status.
""",
    ))
# All 10 spawn in parallel in a single message
```

## Example Output

```
[savage-ralph] STARTUP
  --confirm-burn-budget: OK, operator has acknowledged cost
  mode: MAXIMUM THROTTLE
  sleep: 0s
  max parallel: 10
  model escalation: ON (+1 tier)

  discovered 18 repos (12 from config + 6 additional in ~/src/jbcom/)

━━━ CYCLE 0001 @ 14:32:11 ━━━
  scanning 18 repos in parallel... done in 6s
  queue built: 47 tasks
    14 CI failures
    9 PR fixes
    11 STATE.md features
    8 missing governance files
    5 discovery items (issues labeled ai-welcome)

  batch 1 of 5 — spawning 10 agents:
    [sonnet→]  frontmatter sweep            radioactive-ralph
    [sonnet→]  missing CHANGELOG stub       godot-platformer
    [opus  →]  split orchestrator.py (458L) radioactive-ralph
    [opus  →]  retry logic feature          arcade-cabinet
    [opus  →]  fix flaky integration test   terraform-aws-eks
    [opus  →]  address review comments      assets-mcp#51
    [opus  →]  save-game system             arcade-cabinet
    [opus  →]  resolve merge conflict       godot-platformer#23
    [opus  →]  ARCHITECTURE.md refresh      radioactive-ralph
    [opus  →]  lint fix                     terraform-aws-eks#87

  batch 1 done in 11m 48s
  cost estimate so far: $18.22

  merging MERGE_READY:
    ✓ radioactive-ralph#149  ✓ arcade-cabinet#78  ✓ terraform-aws-eks#87
    ✓ radioactive-ralph#151  ✓ godot-platformer#23

  NO SLEEP. cycle 0002 starting immediately...

━━━ CYCLE 0002 @ 14:44:03 ━━━
  (rescanning, queue rebuilt with 29 remaining tasks)
  ...

  [hourly cost tick] $42.16 spent in first hour. 23 PRs merged.
```

## Why savage-ralph exists

Because sometimes "normal autonomous" isn't fast enough. Savage-ralph is the nuclear option — it exists specifically for high-urgency, high-budget, "I don't care how much it costs, get it done" scenarios. It's not your default. It's not your daily driver. It's the thing you pull out of the glass case and hit with a hammer when the normal tools aren't moving fast enough.
