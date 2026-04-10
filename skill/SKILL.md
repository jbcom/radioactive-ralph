---
name: radioactive-ralph
description: "Fully autonomous continuous development loop using radioactive-ralph logic. Scans PRs across all configured repos, merges green ones, reviews pending ones, discovers work from STATE.md and missing files, then spawns Claude Code agents to execute the highest-priority batch. Loops until interrupted. Triggers: /radioactive-ralph, 'start radioactive-ralph', 'run ralph', 'autonomous loop'."
argument-hint: "[--config <path>] [--once] [--focus <repo-name>]"
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
  - TaskOutput
  - TaskStop
  - SendMessage
---

# /radioactive-ralph — Autonomous Continuous Development Loop

**radioactive-ralph running inside Claude Code.** This skill executes the same orchestration logic as `ralph run` (the external daemon) but uses the Agent tool to spawn subagents within this session — no separate API key or process required.

The two execution modes are equivalent in behavior. Choose based on what fits your workflow:

| Mode | How to start | Best for |
|------|-------------|---------|
| **Skill (this)** | `/radioactive-ralph` in Claude Code | Interactive sessions, no API key setup |
| **Daemon** | `ralph run` in terminal | Background 24/7, survives session end |

---

## Core Loop

```
while not interrupted:
  1. scan_prs()          → classify all open PRs across all repos
  2. drain_merge_queue() → squash-merge any MERGE_READY PRs
  3. review_pending()    → AI-review PRs that NEEDS_REVIEW
  4. discover_work()     → read STATE.md, find missing files, rank items
  5. execute_batch()     → spawn agents for highest-priority work
  6. sleep(30s)          → adaptive backoff on errors
```

---

## Model Selection

| Task | Model |
|------|-------|
| Architecture decisions, security review | `opus` |
| Feature work, bug fixes, PR review | `sonnet` |
| Doc sweeps, missing files, bulk mechanical work | `haiku` |

---

## Step 1 — Discover Configuration

Read config from `~/.radioactive-ralph/config.toml`. If absent, discover repos from standard paths:

```bash
# Check for config
cat ~/.radioactive-ralph/config.toml 2>/dev/null || echo "NO_CONFIG"

# Standard org paths (used if no config)
ls ~/src/ 2>/dev/null
```

Repo discovery: for each configured org directory, find all subdirectories containing `.git/`.

---

## Step 2 — Scan PRs

For each repo, get open PRs using the appropriate forge CLI:

```bash
# GitHub
gh pr list --repo <owner/repo> --json number,title,author,headRefName,isDraft,url,updatedAt --limit 50

# GitLab (if glab available)
glab mr list --repo <project> --output json

# Gitea/Forgejo: use HTTP API directly via curl
```

Classify each PR:
- **MERGE_READY**: CI passing + approved + not draft + no unresolved comments
- **NEEDS_REVIEW**: CI passing + no approval yet
- **NEEDS_FIXES**: Has requested changes or review comments
- **CI_FAILING**: CI failed
- **DRAFT**: is_draft = true
- **STALE**: not updated in 14+ days

---

## Step 3 — Merge Ready PRs

For each MERGE_READY PR, squash-merge via forge CLI:

```bash
# GitHub
gh pr merge <number> --repo <owner/repo> --squash --delete-branch

# GitLab
glab mr merge <iid> --squash --remove-source-branch

# After merge, pull main:
git -C <repo_path> pull origin main
```

Log: "Merged PR #N in <repo>"

---

## Step 4 — Review Pending PRs

For each NEEDS_REVIEW PR, spawn a `sonnet` reviewer agent:

```
Agent(
  model="sonnet",
  prompt="""
  Review PR #<N> in <repo_path>.
  
  Get the diff: gh pr diff <N> --repo <owner/repo>
  
  Assess:
  1. Does it follow project standards (STANDARDS.md)?
  2. Are there bugs, security issues, or regressions?
  3. Is the logic sound?
  
  If approved: gh pr review <N> --approve --body "<summary>"
  If needs fixes: gh pr review <N> --request-changes --body "<specific feedback>"
  
  Be concrete. Don't nitpick style.
  """
)
```

---

## Step 5 — Discover Work

For each repo:

1. Read `docs/STATE.md` for explicit "next" items
2. Check for missing required files: CLAUDE.md, AGENTS.md, README.md, CHANGELOG.md, STANDARDS.md
3. Check for missing required docs: docs/ARCHITECTURE.md, docs/DESIGN.md, docs/TESTING.md, docs/STATE.md
4. Check for CI failures in recent PRs
5. Check for PRs with requested changes

Rank by priority (lower = higher priority):
1. CI_FAILURE
2. PR_FIXES (PRs with requested changes)  
3. DOC_SWEEP (missing docs across many repos)
4. MISSING_FILES (required project files)
5. STATE_NEXT (items from STATE.md)
6. DESIGN_FEATURE (new features)
7. POLISH

---

## Step 6 — Execute Batch

Take up to `max_parallel_agents` (default: 5) highest-priority items. For each, spawn an agent:

```
Agent(
  model=<select_model(task.priority)>,
  description="<task.description> in <repo_name>",
  prompt="""
  Working in: <repo_path>
  Task: <task.description>
  Context: <task.context>
  
  Rules:
  - Work on a branch, open a PR when done
  - Follow STANDARDS.md in this repo  
  - Run tests before opening PR
  - Use conventional commit messages
  - If task is unclear, do your best interpretation and note it in the PR
  """,
  isolation="worktree"
)
```

Wait for all agents to complete. Log results. If an agent created a PR, note the URL.

---

## Execution Arguments

- `--config <path>`: Use alternative config file (default: `~/.radioactive-ralph/config.toml`)
- `--once`: Run a single cycle then stop (useful for testing)
- `--focus <repo>`: Only process the named repo

---

## Error Handling

- **Single task failure**: Log and continue, don't stop the loop
- **Forge API failure**: Log, skip that repo, continue
- **Consecutive errors (3+)**: Increase sleep to 5 min, alert user
- **Consecutive errors (10+)**: Stop and report — something is fundamentally broken

---

## State Persistence

Read and write `~/.radioactive-ralph/state.json` to track:
- `work_queue`: Pending work items (deduped by content hash)
- `active_runs`: Currently running agents
- `completed_runs`: Results from completed agents (keep last 100)
- `cycle_count`: How many cycles have run
- `last_scan` / `last_discovery`: Timestamps

This enables the skill to resume from where it left off after context compaction.

---

## Starting the Loop

```
1. Read ~/.radioactive-ralph/state.json (or create empty state)
2. Recover orphaned active_runs (re-queue if started > 2h ago)
3. Announce: "Starting radioactive-ralph — cycle N"
4. Run _cycle()
5. Sleep 30s (or longer on errors)
6. Loop
```

When the user interrupts (`/stop`, Ctrl+C, new message), finish the current cycle cleanly and persist state.

---

## Example Output

```
=== Cycle 1 ===
Scanning 12 repos for PRs...
Found 3 open PRs across 3 repos

Merging 1 ready PR...
  Merged PR #47 in kings-road

Reviewing 1 PR...
  PR #23 in radioactive-ralph: approved (no blocking issues)

Discovering work...
  Added 4 new work items (queue: 7)

Executing 5 agents...
  [haiku]  kings-road: Create missing CHANGELOG.md
  [haiku]  aetheria: Create missing docs/STATE.md
  [sonnet] radioactive-ralph: Fix work_discovery hash collision bug
  [sonnet] psyducks-infinite-headache: Implement config validation
  [opus]   jbcom.github.io: Design caching architecture for asset pipeline

Agents complete. 4/5 succeeded. 1 PR created: https://github.com/...

Sleeping 30s...
=== Cycle 2 ===
```
