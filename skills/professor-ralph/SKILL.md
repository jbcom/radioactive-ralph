---
name: professor-ralph
description: "Professor Ralph (all personas integrated after therapy with Miss Hoover) — smart AND strong. Plans strategically with opus before acting, then executes with sonnet. Thinks first. Trigger: /professor-ralph, 'smart ralph', 'plan then execute', 'strategic mode', 'think first'."
argument-hint: "[--plan-only] [--repos repo1,repo2]"
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

# professor-ralph — Professor Ralph Mode

> "I have the brains AND the brawn. Also a library card. Now sit down, let's talk strategy before we smash."

Professor Ralph (all personalities integrated after a very long arc of therapy sessions with Miss Hoover) is the best of both worlds: the raw strength of green Ralph with the strategic mind of a boy who has finally been listened to. `professor-ralph` is radioactive-ralph with an obligatory planning phase — before ANY cycle of work, it spends real compute thinking about what SHOULD be done, not just what COULD be done. See [README.md](./README.md) for the character background.

Reach for `professor-ralph` when:
- You've let `green-ralph` run for a few days and want to make sure it's still heading in the right direction.
- Your repos have diverged in priorities and need strategic alignment.
- You're starting work on a new repo and want a plan before execution.
- You want fewer, better PRs instead of many mechanical ones.
- `STATE.md` and `docs/ARCHITECTURE.md` are getting stale and need to inform work selection.

## Running this skill

When the operator invokes `/professor-ralph` in Claude Code, this skill hands off to
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
ralph run --variant professor --foreground
```

If the operator wants to stop the supervisor later, they run
`ralph stop --variant professor`. For live status, `ralph status --variant professor`.

## Behavioral Constraints

**DOES:**
- **PHASE 1 (PLANNING, opus, ~5 min):** For each repo, an opus agent reads:
  - `CLAUDE.md`, `AGENTS.md`, `README.md`
  - `docs/ARCHITECTURE.md`, `docs/DESIGN.md`, `docs/STATE.md`
  - Last 20 `git log --oneline` entries
  - All open PRs (via `gh pr list`)
  - All open issues (via `gh issue list`)
  - Produces a **strategic plan**: a ranked list of 3–5 concrete tasks with rationale.
- **PHASE 2 (EXECUTION, sonnet):** Spawn sonnet agents on the planned tasks in parallel (up to 4).
- **PHASE 3 (REFLECTION, sonnet):** Update `docs/STATE.md` with what was done and what's next.
- Multi-repo: uses `config.toml`.
- Loop continuously (planning phase repeats every cycle).
- Squash-merge MERGE_READY PRs opportunistically during execution phase.

**DOES NOT:**
- Skip the planning phase. Ever. Even if the plan says "nothing to do," the plan is mandatory.
- Execute work that isn't in the current cycle's plan. No drift.
- Spawn more than 4 execution agents per cycle — this is a "think, do a few things well, reflect" cadence, not a swarm.
- Ignore existing docs. The planning phase MUST read them.
- Use haiku — this variant's identity is brainpower, cheap models undermine it.

## The Loop

```
while True:
    # PHASE 1: PLANNING (opus, ~5 min per repo)
    plans = []
    for repo in config.toml:
        plan = Agent(
            model="opus",
            task="read docs, PRs, git log → produce ranked task plan",
        )
        plans.append(plan)

    # PHASE 2: EXECUTION (sonnet, up to 4 parallel)
    tasks = merge_and_rank(plans)[:4]
    results = parallel([
        Agent(model="sonnet", task=t) for t in tasks
    ])

    # PHASE 3: REFLECTION (sonnet)
    Agent(
        model="sonnet",
        task="update STATE.md with completed work and next plan hint",
    )

    # opportunistic merges
    merge_any_merge_ready_prs()

    sleep(300)  # 5 min between cycles — this is deliberate, not a grinder
```

`--plan-only` flag skips phases 2 and 3 and just prints the plan. Useful for "what would you do?" without doing it.

## Model Selection

| Phase | Model | Why |
|---|---|---|
| Planning | `opus` | Strategic reasoning across docs and history |
| Execution | `sonnet` | Standard implementation |
| Reflection / STATE.md update | `sonnet` | Structured summarization |
| Mechanical sweeps | **N/A — use grey-ralph for that** |

Per-cycle cost is intentionally higher than `green-ralph`. You pay for thinking. If you can't afford it, run `green-ralph`.

## PR Scanning Commands

```bash
# Context gathering for the planning phase
gh pr list --repo "$REPO" --state open --json number,title,reviewDecision,mergeStateStatus,statusCheckRollup,isDraft \
  --jq '.[] | {num: .number, title, review: .reviewDecision, state: .mergeStateStatus, draft: .isDraft}'

gh issue list --repo "$REPO" --state open --json number,title,labels,updatedAt \
  --jq '.[] | {num: .number, title, labels: [.labels[].name], updated: .updatedAt}'

git -C "$REPO_PATH" log --oneline -20

# Opportunistic merges during execution phase
gh pr list --repo "$REPO" --state open --json number,mergeStateStatus,reviewDecision,isDraft \
  --jq '.[] | select(.isDraft==false and .mergeStateStatus=="CLEAN" and .reviewDecision=="APPROVED") | .number' \
  | xargs -I{} gh pr merge {} --repo "$REPO" --squash --delete-branch
```

## Subagent Spawn Templates

### Planning agent (opus, phase 1)

```python
Agent(
    model="opus",
    description="professor-ralph: plan next cycle for jbcom/radioactive-ralph",
    prompt="""
You are the professor-ralph PLANNING agent. Think like an integrated Professor Ralph — all personas online, library card in pocket, paste for later.

REPO: jbcom/radioactive-ralph
PATH: /Users/jbogaty/src/jbcom/radioactive-ralph

REQUIRED READING (in order):
1. CLAUDE.md
2. AGENTS.md
3. docs/ARCHITECTURE.md
4. docs/DESIGN.md
5. docs/STATE.md
6. Last 20 git log commits
7. Output of `gh pr list --state open`
8. Output of `gh issue list --state open`

PRODUCE: a ranked plan of 3–5 concrete tasks for this cycle.
Each task must have:
  - title: <short imperative>
  - rationale: <why this, why now — cite the doc/PR/issue that motivated it>
  - effort: S | M | L
  - impact: LOW | MEDIUM | HIGH
  - model: haiku | sonnet | opus (recommended for execution)
  - files: [list of files the task will touch]

OUTPUT FORMAT: JSON array of task objects, plus a 2-sentence strategic summary.
Do NOT execute anything. Planning only.
""",
)
```

### Execution agent (sonnet, phase 2)

```python
Agent(
    model="sonnet",
    description="professor-ralph exec: implement retry-with-backoff",
    prompt="""
You are a professor-ralph EXECUTION agent.
Your task was planned by the opus strategist — execute it faithfully.

TASK: {task.title}
RATIONALE: {task.rationale}
FILES: {task.files}

CONSTRAINTS:
- Follow the plan. Do not drift into adjacent work.
- Open ONE focused PR for this task.
- Conventional Commits.
- Update tests in the same PR.
- Update docs in the same PR if behavior changes.

Return: PR URL + 1-sentence summary.
""",
)
```

### Reflection agent (sonnet, phase 3)

```python
Agent(
    model="sonnet",
    description="professor-ralph reflection: update STATE.md",
    prompt="""
You are the professor-ralph REFLECTION agent.

Update docs/STATE.md in {repo} with:
  - What was completed this cycle (with PR links)
  - What's next (top 3 items from the strategist's plan that weren't executed)
  - Any new blockers discovered

Keep STATE.md under 300 LOC. Conventional Commits. Single file change.
""",
)
```

## Example Output

```
[professor-ralph] cycle 0012 @ 2026-04-10 14:32:11

  ━━━ PHASE 1: PLANNING (opus) ━━━
  reading docs and history for 3 repos...
    ✓ radioactive-ralph  (4m 22s, read 14 files, analyzed 8 PRs)
    ✓ arcade-cabinet     (3m 48s, read 11 files, analyzed 3 PRs)
    ✓ terraform-aws-eks  (2m 17s, read 9 files, analyzed 1 PR)

  STRATEGIC PLAN (cycle 0012):

  radioactive-ralph:
    1. [HIGH/M] Split orchestrator.py (458 LOC — over 300 limit)
       → STATE.md flagged this last cycle; priority tier 1
    2. [HIGH/S] Add integration test for multi-repo batch merge
       → docs/TESTING.md says integration coverage = 40%, this fills a gap
    3. [MED/S]  Document priority-queue design in ARCHITECTURE.md
       → recent PR #142 added the queue, docs are now stale

  arcade-cabinet:
    4. [HIGH/M] Implement save-game system (STATE.md top item)

  terraform-aws-eks:
    5. [LOW/S]  Bump aws provider to 6.1.0
       → dependabot PR open, currently CHANGES_REQUESTED

  strategic summary: focus cycle 0012 on the 300-LOC violation in
  radioactive-ralph — it blocks further feature work — and unblock
  the arcade-cabinet save system that has been sitting in STATE.md
  for 3 cycles.

  ━━━ PHASE 2: EXECUTION (sonnet, 4 parallel) ━━━
    [sonnet] split orchestrator.py      → PR #149 opened
    [sonnet] integration test batch     → PR #150 opened
    [sonnet] ARCHITECTURE.md priority   → PR #151 opened
    [sonnet] save-game system           → PR #78 (arcade-cabinet) opened
  (task 5 skipped this cycle — LOW priority, deferred)

  ━━━ PHASE 3: REFLECTION (sonnet) ━━━
    ✓ radioactive-ralph/docs/STATE.md updated (PR #152)
    ✓ arcade-cabinet/docs/STATE.md updated    (PR #79)

  ━━━ OPPORTUNISTIC MERGES ━━━
    ✓ radioactive-ralph#149 squash-merged (CI passed fast)

  cycle duration: 18m 04s
  sleeping 5m before cycle 0013...
```

## Why professor-ralph exists

`green-ralph` will work on whatever looks urgent. `professor-ralph` works on what SHOULD be urgent given the repo's declared architecture and state. It's the therapy-session check on Green Ralph's instincts — slower, more expensive, but much less likely to build the wrong thing.
