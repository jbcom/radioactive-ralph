---
name: world-breaker-ralph
description: "Post-Wumple-Wuzzle grief weaponized. Maximum parallelism across ALL repos simultaneously, every agent at opus, no sleep, no mercy. Reserved for when you have lost something and the only response is to make everything else right. Triggers: /world-breaker-ralph, 'i need everything fixed now', 'maximum effort', 'all of it'."
argument-hint: "[--confirm-burn-everything]"
user-invocable: true
allowed-tools:
  - Agent
  - Bash
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - TaskCreate
  - TaskUpdate
  - TaskList
---

# /world-breaker-ralph — The World Breaker Ralph

> *"I don't want to hurt you. I don't want to help you. But if you get in my way... if ANY of you get in my way..."*
> — World Breaker Ralph, World War Ralph #1

See [README.md](./README.md) for the full character background.

---

## Running this skill

When the operator invokes `/world-breaker-ralph` in Claude Code, this skill hands off to
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
ralph run --variant world-breaker --foreground --confirm-burn-everything
```

If the operator wants to stop the supervisor later, they run
`radioactive_ralph stop --variant world-breaker`. For live status, `radioactive_ralph status --variant world-breaker`.

## ⚠️ WORLD-BREAKER-RALPH. READ THIS FIRST.

This is not a daily driver. This is not for "I have a lot to do." This is for when something broke badly enough that the normal pace is unacceptable and cost is not a factor.

World-breaker-ralph runs **every agent at opus**, **maximum parallelism (10)**, **across all repos simultaneously**, with **no sleep between cycles**. A single run can consume significant API budget.

Requires `--confirm-burn-everything`.

---

## When to use this

- A critical incident has occurred and you need everything fixed across the whole portfolio NOW
- A major architecture decision has been made and you need it propagated everywhere immediately  
- You've been blocked for days and want one overwhelming push to clear the backlog
- The codebase is in a bad state across many repos and you want it resolved in one session

## When NOT to use this

- Normal work
- Budget matters
- You have time

---

## What makes it different

| Setting | green-ralph | savage-ralph | world-breaker-ralph |
|---------|-------------|--------------|---------------------|
| Parallelism | 5–6 | 10 | 10 |
| Default model | sonnet | escalated | **opus across the board** |
| Sleep between cycles | 30s | 0 | 0 |
| Scope | configured repos | configured + discovered | **all repos** |
| PR reviews | sonnet | sonnet | **opus** |
| Merge decisions | standard | standard | **opus** |
| Budget warning | no | yes | **hard gate** |

---

## Startup Gate

```
⚠️  WORLD-BREAKER-RALPH
════════════════════════════════════════════════════════
Every agent runs at opus.
10 parallel agents.
Zero sleep between cycles.
All repos in scope simultaneously.

This will consume significant budget. There is no undo on that.

Run with --confirm-burn-everything to proceed.
════════════════════════════════════════════════════════
```

---

## The Loop

```
1. discover_all()    → find every git repo under configured orgs
2. scan_all_prs()    → opus-reviewed classification of all PRs
3. merge_all_ready() → merge everything MERGE_READY simultaneously
4. review_all()      → opus review of every NEEDS_REVIEW PR
5. discover_work()   → full work discovery across all repos
6. execute_10()      → 10 parallel opus agents
7. sleep(0)          → immediately start next cycle
8. loop
```

---

## Model Selection

Everything is opus. The distinction between task types that other ralphs use doesn't apply here — when the world is breaking, you don't send a haiku.

Exception: git operations (pull, push, checkout) run as shell commands, not agents.

---

## Agent Spawn Template

```
Agent(
  model="opus",
  description="[world-breaker] <task> in <repo>",
  prompt="""
  Working in: <repo_path>
  Task: <description>
  
  This is a world-breaker execution. Prioritize:
  1. Correctness over caution
  2. Completeness over incrementalism
  3. Fix the root cause, not the symptom
  
  Do not leave partial work. Open a PR when done.
  Make it right.
  """
)
```

---

## Example Output

```
=== world-breaker-ralph: cycle 1 ===
⚠️  CONFIRMED: --confirm-burn-everything. Deploying.

Scope: 23 repos across 3 orgs

Scanning all PRs... 31 open across 18 repos
  MERGE_READY: 6
  NEEDS_REVIEW: 9
  NEEDS_FIXES: 8
  CI_FAILING: 8

Merging 6 ready PRs... done.

Reviewing 9 PRs (opus)...
  PR #47 kings-road: approved
  PR #23 radioactive-ralph: 2 blocking findings
  [... 7 more ...]

Discovering work... 34 items across all repos

Executing 10 opus agents simultaneously...
  kings-road:          Fix CI failure in integration tests
  radioactive-ralph:   Address PR #23 review findings
  psyducks:            Create missing docs/ARCHITECTURE.md
  aetheria:            Implement STATE.md next item: user auth
  [... 6 more ...]

Cycle 1 complete. 10/10 agents succeeded. 4 PRs created.
Starting cycle 2 immediately.

=== world-breaker-ralph: cycle 2 ===
```
