---
name: immortal-ralph
description: "Immortal Ralph — crash-resistant, always comes back. Retries on every error, persists state obsessively, never dies. Sonnet only. Conservative. Trigger: /immortal-ralph, 'never stop ralph', 'resilient mode', 'keep going no matter what', 'set and forget'."
argument-hint: "[--state-file path]"
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

# immortal-ralph — Immortal Ralph Mode

> "You can't send me to the principal's office. You can only slow me down. I come back at 8:15 AM."

Immortal Ralph (Miss Hoover's horror arc, inspired by Al Ewing's Immortal Hulk run) literally cannot be expelled — whatever happens to him, he comes back through the supply closet the next morning. `immortal-ralph` is radioactive-ralph built for marathons, not sprints: maximum resilience, obsessive state persistence, conservative model choices, and a retry loop that treats every error as temporary. Where `green-ralph` loops for hours, `immortal-ralph` loops for days or weeks, surviving network blips, API rate limits, transient git failures, and anything else the world throws at it. See [README.md](./README.md) for the character background.

Reach for `immortal-ralph` when:
- You want "set and forget" autonomous work for a long weekend.
- Your network is flaky or you're on unreliable VPN.
- You've been burned by `green-ralph` dying on a transient gh API hiccup.
- Budget-conscious: sonnet only, no opus spikes.
- You want forensics — every sub-step is journaled to disk.

## Running this skill

This skill drives `immortal-ralph` via the `radioactive_ralph` MCP server.
The server is registered as an MCP endpoint Claude Code reads on startup
(see `.claude/settings.json` in the operator's repo or globally).

When the operator invokes `/immortal-ralph`, walk through these steps:

```bash
# 1. Verify the binary is installed (the MCP server runs under it).
if ! command -v radioactive_ralph >/dev/null 2>&1; then
  cat <<'EOS'
radioactive_ralph is not installed on PATH. Install via:

  brew tap jbcom/pkgs && brew install radioactive-ralph    # macOS / Linux
  scoop bucket add jbcom https://github.com/jbcom/pkgs && scoop install radioactive-ralph    # Windows
  choco install radioactive-ralph                              # Windows (chocolatey)
EOS
  exit 1
fi

# 2. Verify the repo is initialized. Idempotent — also seeds an active
#    plan in plandag so the plans-first gate passes.
radioactive_ralph init --yes
```

Then call the MCP tools (the outer Claude invokes these through the
registered `radioactive_ralph` MCP server — no shell-out from the skill):

1. `plan.list` to discover the active plan id (or pick by slug)
2. `plan.next` with `variant: "immortal"` to see what's ready
3. `variant.spawn` with `variant_name: "immortal"` to launch a subprocess
4. `plan.claim` to atomically check out a task for the new variant
5. Iterate: read variant output, call `variant.say` to feed it
   guidance, watch the plan DAG advance via `plan.show`
6. `variant.kill` when the plan is exhausted or operator stops the run

The MCP server keeps the plandag DB warm across calls, owns the variant
pool, writes heartbeat rows, and reaps dead subprocesses on the next
invocation.
## Behavioral Constraints

**DOES:**
- Loop FOREVER. The only way to stop it is explicit operator interrupt.
- Persist state to `~/.local/share/radioactive-ralph/immortal-state.json` (or `--state-file` path) after EVERY sub-step — not every cycle, every sub-step.
- On startup, load the state file and RESUME where it left off.
- On any Python exception / bash non-zero / subagent failure: log, sleep 60s, retry the exact same operation.
- Track `consecutive_failures` counter. On 10 consecutive failures: take a 30-minute cooldown break, reset counter, resume.
- Use `sonnet` for EVERYTHING. No haiku (unreliable for nuanced tasks), no opus (budget discipline).
- Prefer safe, small, reversible changes. Skip anything that looks risky.
- Squash-merge MERGE_READY PRs — this is still a productive loop, just a resilient one.
- Log every event to `~/.local/share/radioactive-ralph/immortal.log` with timestamps.

**DOES NOT:**
- Exit on error. Ever.
- Escalate to opus — the whole point is predictable, boring resilience.
- Run risky work: no cross-repo refactors, no ARCHITECTURE.md rewrites, no security-sensitive changes. Those go in the `needs_human` pile and get logged, not executed.
- Spawn more than 3 parallel agents — resilience > speed.
- Forget. State file persistence is non-negotiable.

## State File Format

```json
{
  "version": 1,
  "started_at": "2026-04-10T14:32:11Z",
  "last_heartbeat": "2026-04-10T18:07:42Z",
  "cycle": 284,
  "consecutive_failures": 0,
  "current_operation": {
    "repo": "jbcom/radioactive-ralph",
    "task_id": "ci-fix-142",
    "phase": "running_tests",
    "started_at": "2026-04-10T18:05:12Z"
  },
  "completed_tasks_this_cycle": [
    {"repo": "jbcom/radioactive-ralph", "task": "merge PR #141", "at": "2026-04-10T18:01:22Z"},
    {"repo": "jbcom/arcade-cabinet", "task": "review PR #77", "at": "2026-04-10T18:03:44Z"}
  ],
  "skipped_risky_tasks": [
    {"repo": "jbcom/radioactive-ralph", "task": "refactor orchestrator.py across modules", "reason": "too risky for immortal mode"}
  ],
  "cooldowns_taken": 2,
  "total_prs_merged": 47,
  "total_prs_reviewed": 89
}
```

The state file is written atomically (write to `.tmp`, then `rename`) after every sub-step to survive power loss.

## The Loop (resilient)

```python
STATE_FILE = "~/.local/share/radioactive-ralph/immortal-state.json"
LOG_FILE   = "~/.local/share/radioactive-ralph/immortal.log"

state = load_or_init(STATE_FILE)
log(f"immortal-ralph resuming at cycle {state['cycle']}")

while True:
    try:
        state['cycle'] += 1
        state['current_operation'] = None
        persist(state)

        for repo in config.toml:
            try:
                for task in discover_safe_tasks(repo):
                    state['current_operation'] = {
                        'repo': repo, 'task_id': task.id,
                        'phase': 'starting',
                        'started_at': now(),
                    }
                    persist(state)

                    if is_risky(task):
                        state['skipped_risky_tasks'].append({...})
                        persist(state)
                        continue

                    run_agent(task)  # sonnet
                    state['completed_tasks_this_cycle'].append({...})
                    state['consecutive_failures'] = 0
                    persist(state)
            except Exception as e:
                log(f"repo {repo} error: {e}")
                state['consecutive_failures'] += 1
                persist(state)
                if state['consecutive_failures'] >= 10:
                    log("10 consecutive failures — entering 30min cooldown")
                    state['cooldowns_taken'] += 1
                    persist(state)
                    sleep(1800)
                    state['consecutive_failures'] = 0
                    persist(state)
                else:
                    sleep(60)

        # between cycles, short sleep
        sleep(120)
    except KeyboardInterrupt:
        log("operator interrupt — exiting cleanly")
        persist(state)
        raise
    except Exception as e:
        log(f"top-level error: {e}")
        state['consecutive_failures'] += 1
        persist(state)
        sleep(60)
        # and the while True catches us again
```

## Model Selection

| Task class | Model |
|---|---|
| ALL TASKS | `sonnet` |

No escalation. No exceptions. Predictable cost.

## "Risky" Filter (what gets SKIPPED)

immortal-ralph refuses to run tasks matching ANY of these:

- Cross-repo refactor
- Security-sensitive change (auth, secrets, IAM, `.env`)
- Architecture-level redesign (anything touching `docs/ARCHITECTURE.md` substantively)
- Any single change > 300 LOC in a source file
- Any change to `.github/workflows/*.yml` (CI integrity)
- Any change to `release-please-config.json` or release pipeline
- Any merge conflict resolution requiring judgment

These go in `skipped_risky_tasks` in the state file so a human (or `professor-ralph`) can pick them up later.

## PR Scanning Commands

```bash
# Same primitives as green-ralph, wrapped in retry
retry() {
  local attempts=0
  until "$@"; do
    attempts=$((attempts+1))
    echo "[immortal] retry $attempts for: $*" >&2
    sleep 60
    if [ "$attempts" -ge 10 ]; then
      echo "[immortal] 10 retries — entering cooldown"
      sleep 1800
      attempts=0
    fi
  done
}

retry gh pr list --repo "$REPO" --state open --json number,mergeStateStatus
retry gh pr merge "$PR_NUM" --repo "$REPO" --squash --delete-branch
```

## Subagent Spawn Template

```python
Agent(
    model="sonnet",  # always
    description=f"immortal-ralph: safe task {task.id}",
    prompt="""
You are an immortal-ralph worker. This loop runs for DAYS. Be conservative.

TASK: {task.title}
REPO: {task.repo}

RULES:
- Small, focused, reversible changes only.
- If you encounter ANY ambiguity or risk, stop and return SKIP_RISKY.
- If you hit a flaky failure, return RETRY (parent will retry in 60s).
- Never force push, never --no-verify, never --admin.
- Update tests for any behavior change in the same PR.

OUTPUT (last line):
STATUS: DONE | RETRY | SKIP_RISKY | BLOCKED
""",
)
```

Parent handles `RETRY` by re-invoking the same agent after 60s.

## Example Output

```
[immortal-ralph] resumed from state file, cycle 284
  state: ~/.local/share/radioactive-ralph/immortal-state.json
  log:   ~/.local/share/radioactive-ralph/immortal.log
  uptime: 3 days, 12 hours, 47 minutes
  model: sonnet (exclusive)
  consecutive_failures: 0
  cooldowns_taken: 2
  total_prs_merged: 47

━━━ CYCLE 0284 ━━━
  scanning 12 repos...
    radioactive-ralph: 2 safe tasks, 1 skipped (risky)
    arcade-cabinet:    1 safe task
    terraform-aws-eks: 0 tasks
    ...

  [sub-step] state persisted
  spawning 3 sonnet agents:
    ✓ merge PR #152                       DONE   (42s)
    ✓ review PR #153                      DONE   (1m 18s)
    ✓ frontmatter sweep in docs/          DONE   (2m 04s)
  [sub-step] state persisted

  skipped (logged to state file):
    - orchestrator.py cross-module refactor (risky)

  cycle 284 complete. sleeping 2m.
  [heartbeat] 2026-04-10T18:07:42Z

━━━ CYCLE 0285 ━━━
  [network error] gh api timeout — retrying in 60s
  [sub-step] consecutive_failures: 1
  [sub-step] state persisted
  [network error] gh api timeout — retrying in 60s
  [sub-step] consecutive_failures: 2
  ...
  [recovery] gh api responsive again, consecutive_failures reset
  ...
  scanning 12 repos...
```

## Why immortal-ralph exists

`green-ralph` is fast but brittle — a single unhandled exception can end a long run. `immortal-ralph` trades peak throughput for guaranteed forward progress. It's the marathon runner to green's sprinter. Good for:
- Long weekends away from the laptop
- CI/CD agents that need to survive the real world
- Low-budget persistent autonomy (sonnet-only is cheaper than green-ralph's mixed budget)
- Any scenario where "keep going no matter what" matters more than "go fast"
