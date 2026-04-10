---
name: green-ralph
description: "RALPH SMASH. Classic green Ralph — unlimited loop, full power, all repos, max parallelism. The flagship standard mode for radioactive-ralph. Trigger: /green-ralph, 'run ralph', 'start the orchestrator', 'go unlimited', 'start autonomous loop'."
argument-hint: "[--repos repo1,repo2] [--max-agents N]"
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

# green-ralph — Classic Ralph Mode

> "RALPH IS STRONGEST ONE THERE IS."

Classic green Ralph is the default. Strong, relentless, loops forever, hits every repo in the config, spawns every kind of agent. When someone says "run ralph" with no qualifier, they mean `green-ralph`. This is the flagship mode — the unqualified baseline that every other variant is a specialization of. See [README.md](./README.md) for the character background.

Reach for `green-ralph` when:
- You want the full autonomous orchestrator loop across all configured repos.
- You don't want to think about model selection, parallelism, or priority tiers — just let it rip.
- You're stepping away and want continuous progress until you come back.

## Behavioral Constraints

**DOES:**
- Loop forever until explicitly interrupted (Ctrl-C, shutdown signal).
- Scan ALL repos listed in `~/src/jbcom/radioactive-ralph/config.toml`.
- Squash-merge `MERGE_READY` PRs (green CI, approved, no conflicts).
- AI-review `NEEDS_REVIEW` PRs with sonnet and post review comments.
- Discover work from `STATE.md`, missing governance files, open issues, stale docs.
- Spawn up to 6 parallel subagents per cycle across priority tiers.
- Use the right model for each task (haiku bulk → sonnet features → opus architecture).
- Sleep 60s between cycles to avoid API thrash.

**DOES NOT:**
- Force push, `git reset --hard`, or merge with `--admin`.
- Skip failing CI — a red PR is never merged.
- Touch repos not listed in `config.toml`.
- Ask for permission. Ever. This is full autonomy.

## The Loop

```
while True:
    1. Load config.toml → list of repos
    2. For each repo (in parallel, max 3 repos concurrent):
         a. gh pr list --json → classify PRs
         b. Merge MERGE_READY PRs (squash)
         c. Review NEEDS_REVIEW PRs (sonnet subagent)
         d. Discover work (STATE.md, missing files, issues)
    3. Priority queue the discovered work
    4. Spawn up to 6 agents in parallel on highest-priority items
    5. Wait for batch to finish
    6. Sleep 60s
    7. Goto 1
```

## Model Selection

| Task class | Model | Why |
|---|---|---|
| Doc frontmatter, CHANGELOG sweep, file rename, missing-file creation | `haiku` | Mechanical, cheap, fast |
| Feature implementation, bug fix, PR review, refactor | `sonnet` | Default workhorse |
| ARCHITECTURE.md rewrites, security audit, cross-repo refactor design | `opus` | Only when deep reasoning required |

Rule of thumb: **< 10% of agents should be opus**. If you're spawning more than that, you're over-spending.

## PR Scanning Commands

```bash
# Classify all open PRs in a repo
gh pr list --repo "$REPO" --state open --json number,title,mergeable,mergeStateStatus,reviewDecision,statusCheckRollup,isDraft \
  --jq '.[] | {num: .number, title, mergeable, state: .mergeStateStatus, review: .reviewDecision, draft: .isDraft}'

# MERGE_READY = not draft, CLEAN mergeable, APPROVED review, all green checks
gh pr list --repo "$REPO" --state open --json number,mergeStateStatus,reviewDecision,isDraft,statusCheckRollup \
  --jq '.[] | select(.isDraft==false and .mergeStateStatus=="CLEAN" and .reviewDecision=="APPROVED") | .number'

# Squash merge (never --admin)
gh pr merge "$PR_NUM" --repo "$REPO" --squash --delete-branch

# NEEDS_REVIEW = not draft, no review yet, CI passing
gh pr list --repo "$REPO" --state open --json number,reviewDecision,isDraft,statusCheckRollup \
  --jq '.[] | select(.isDraft==false and .reviewDecision==null) | .number'
```

## Subagent Spawn Template

```python
Agent(
    model="sonnet",  # or haiku/opus per table above
    description="Fix CI failure in radioactive-ralph PR #42",
    prompt="""
You are a green-ralph worker subagent. Your task:

REPO: jbcom/radioactive-ralph
BRANCH: feat/orchestrator-retry-logic
TASK: CI is failing on the lint step. Fix the root cause, commit, push.

CONSTRAINTS:
- Never force push
- Never --no-verify
- Conventional Commits (fix:/feat:/chore:)
- Address root cause, don't suppress warnings
- Update tests if you change behavior

When done, output a one-line status: FIXED | BLOCKED | NEEDS_HUMAN
""",
)
```

Spawn in parallel batches with `ThreadPoolExecutor` or multiple `Agent` calls in one message.

## Example Output

```
[green-ralph] cycle 0047 @ 2026-04-10 14:32:11
  scanning 12 repos from config.toml
  ├─ radioactive-ralph:    2 MERGE_READY, 1 NEEDS_REVIEW, 3 discovered
  ├─ arcade-cabinet:       0 MERGE_READY, 0 NEEDS_REVIEW, 1 discovered
  ├─ terraform-aws-eks:    1 MERGE_READY, 0 NEEDS_REVIEW, 0 discovered
  └─ ...
  merging 3 PRs:
    ✓ radioactive-ralph#142 "feat: add priority queue" (squash)
    ✓ radioactive-ralph#143 "docs: update STATE.md"    (squash)
    ✓ terraform-aws-eks#87  "chore: bump provider"     (squash)
  reviewing 1 PR:
    ✓ radioactive-ralph#144 "refactor: split orchestrator.py" → APPROVED w/ 2 comments
  spawning 6 agents:
    [haiku]  frontmatter sweep in docs/              → eta 2m
    [haiku]  missing CHANGELOG.md in 3 repos          → eta 3m
    [sonnet] implement retry logic (STATE.md item)   → eta 12m
    [sonnet] fix lint failure in arcade-cabinet#99   → eta 5m
    [sonnet] update ARCHITECTURE.md cross-refs       → eta 8m
    [opus]   design multi-repo dependency graph     → eta 20m
  batch complete in 18m 42s
  sleeping 60s before cycle 0048...
```

## Termination

`green-ralph` only stops on:
1. SIGINT / Ctrl-C from operator
2. SIGTERM from supervisor
3. Unrecoverable auth failure (gh token expired) — exits with clear error

On any recoverable error: log, sleep 60s, continue the loop. Never die on a single failure.
