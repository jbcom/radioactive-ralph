---
name: ralph
description: "Lightweight single-repo autonomous loop. Scans PRs in the current repo, merges green ones, reviews pending ones, discovers the next work item from STATE.md or missing files, and executes it. Simpler than /radioactive-ralph — no multi-org config, no state file, just 'do the next thing here'. Triggers: /ralph, 'do the next thing', 'what's next here'."
argument-hint: "[--once] [--repo <path>]"
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

# /ralph — Single-Repo Autonomous Loop

A lightweight, zero-config version of the radioactive-ralph loop. Works on a single repo (the current working directory or `--repo <path>`) with no config file required.

For multi-repo orchestration across an org portfolio, use [/radioactive-ralph](../radioactive-ralph/SKILL.md) instead.

---

## When to use which

| | `/ralph` | `/radioactive-ralph` |
|---|---------|---------------------|
| Scope | Current repo | All configured org repos |
| Config needed | No | `~/.radioactive-ralph/config.toml` |
| State file | No | `~/.radioactive-ralph/state.json` |
| Best for | Single project sessions | Continuous background orchestration |

---

## The Loop

```
1. detect_repo()      → find the git repo root (cwd or --repo)
2. scan_prs()         → classify open PRs in this repo
3. merge_ready()      → squash-merge any MERGE_READY PRs
4. review_pending()   → AI-review PRs that need review
5. discover_next()    → read STATE.md, find missing required files
6. execute()          → spawn one agent for highest-priority item
7. if --once: stop; else: loop
```

---

## Step 1 — Find the Repo

```bash
# Find repo root from cwd
git rev-parse --show-toplevel

# Or use --repo argument if provided
```

Detect forge from remote URL:
```bash
git remote get-url origin
# → git@github.com:org/repo → GitHub
# → git@gitlab.com:org/repo → GitLab
# → git@gitea.example.com:org/repo → Gitea/Forgejo
```

---

## Step 2 — Classify PRs

```bash
# GitHub
gh pr list --json number,title,author,headRefName,isDraft,url,updatedAt --limit 50

# GitLab
glab mr list --output json

# Check CI for each open PR
gh pr checks <number>
```

Classification:
- **MERGE_READY**: CI green + approved + not draft
- **NEEDS_REVIEW**: CI green + no review yet
- **NEEDS_FIXES**: Changes requested
- **CI_FAILING**: CI failed
- **DRAFT**: is_draft = true

---

## Step 3 — Merge Ready PRs

```bash
gh pr merge <number> --squash --delete-branch
# or: glab mr merge <iid> --squash --remove-source-branch

git pull origin main
```

---

## Step 4 — Review Pending PRs

Spawn a `sonnet` agent:

```
Agent(
  model="sonnet",
  prompt="""
  Review PR #<N> in <repo_path>.
  
  Get the diff and assess:
  - Bugs or regressions
  - Security issues
  - Adherence to STANDARDS.md
  
  gh pr review <N> --approve --body "..." 
  # or --request-changes --body "..."
  
  Be concrete. Don't nitpick style.
  """
)
```

---

## Step 5 — Discover Next Work

Priority order:

1. **CI_FAILURE** — any PR with failing CI
2. **PR_FIXES** — PRs with requested changes (use `gh pr view <N>`)
3. **MISSING_FILES** — check for: CLAUDE.md, AGENTS.md, README.md, CHANGELOG.md, STANDARDS.md, docs/ARCHITECTURE.md, docs/DESIGN.md, docs/TESTING.md, docs/STATE.md
4. **STATE_NEXT** — parse `docs/STATE.md` for lines starting with `- [ ]` or `## Next`
5. **POLISH** — if nothing else, look for obvious improvements (test coverage, stale docs)

---

## Step 6 — Execute

Spawn one agent for the top item. Model selection:

| Priority | Model |
|----------|-------|
| CI_FAILURE, PR_FIXES | `sonnet` |
| MISSING_FILES (doc files) | `haiku` |
| STATE_NEXT (features) | `sonnet` |
| Architecture, design | `opus` |

```
Agent(
  model=<selected>,
  description="<task description>",
  prompt="""
  Working in: <repo_path>
  Task: <description>
  Context: <context from STATE.md or file scan>
  
  Rules:
  - Work on a branch, open a PR when done
  - Follow STANDARDS.md in this repo
  - Run tests before opening PR
  - Conventional commit messages
  """,
  isolation="worktree"
)
```

---

## Arguments

- `--once`: Run one cycle and stop (default: loop)
- `--repo <path>`: Target repo path (default: current working directory)

---

## Example Output

```
=== ralph: single-repo loop ===
Repo: ~/src/my-project (GitHub: org/my-project)

Scanning PRs... 2 open
  #12 Create login page       → NEEDS_REVIEW
  #11 Fix config parsing bug  → MERGE_READY

Merging PR #11...  Done.

Reviewing PR #12...  Approved (no blocking issues).

Discovering next work...
  Missing: docs/STATE.md
  Missing: CHANGELOG.md
  STATE.md next: "Add password reset flow"

Executing: Create missing CHANGELOG.md [haiku]

Agent done. PR created: https://github.com/org/my-project/pull/13

Sleeping 30s...
```
