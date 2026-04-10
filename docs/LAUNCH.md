---
title: Launch
updated: 2026-04-10
status: current
domain: creative
---

# Launch Plan — radioactive-ralph

Everything needed before the LinkedIn / Hacker News drop. Flip boxes as items ship. Drafts for the social copy live at the bottom.

## Checklist

### Visual assets
- [x] Hero image optimized (`assets/ralph-mascot.png`)
- [ ] Social preview uploaded (`assets/social-preview.png`, 1280x640 PNG — then GitHub Settings -> Social preview)
- [ ] Demo GIF recorded (`assets/demo.gif`, via `scripts/record-demo.sh`)
- [ ] Per-variant icon set (`assets/variants/*.svg`, ten files)
- [ ] Architecture diagram SVG (`assets/architecture.svg`)

### Documentation
- [x] README polished — hero, two install paths, ten-variant table, personality block, launch notes collapsible
- [x] All ten variant READMEs polished (green, grey, red, blue, professor, savage, immortal, joe-fixit, old-man, world-breaker — each with Character + Skill sections)
- [x] `skills/README.md` variants index
- [x] `AGENTS.md`, `STANDARDS.md`, `docs/ARCHITECTURE.md`, `docs/DESIGN.md`, `docs/STATE.md`, `docs/TESTING.md`
- [x] `CHANGELOG.md` current
- [x] `assets/ASSETS.md` — asset manifest with production instructions

### Packaging
- [x] Plugin manifest (`.claude-plugin/`)
- [x] `release-please-config.json` + manifest
- [ ] `install.sh` tested on clean macOS (arm64)
- [ ] `install.sh` tested on clean Ubuntu 24.04 (x86_64)
- [ ] `uvx radioactive-ralph run` tested from clean environment

### Demo verification
- [ ] `/green-ralph` runs end-to-end (single cycle, no errors)
- [ ] `/red-ralph` runs end-to-end on a repo with a known CI failure
- [ ] `/joe-fixit-ralph --cycles 1` runs end-to-end and prints the bill
- [ ] `ralph status`, `ralph discover`, `ralph pr list` all return within 5s on an empty state

### Copy
- [ ] LinkedIn post drafted — see below
- [ ] Hacker News Show post drafted — see below
- [ ] Replies pre-written for the three most likely HN objections (see "Expected objections" below)

## Draft — LinkedIn post

> I shipped an autonomous development orchestrator for Claude Code. It runs continuously across a portfolio of git repos — scans for work, reviews PRs, addresses feedback, spawns Claude subagents to fix whatever needs fixing, merges what's ready, and goes back to sleep.
>
> It is named radioactive-ralph. The personality module logs every status line in the voice of Ralph Wiggum. Yes, that Ralph Wiggum. My cat's breath smells like cat food. Cycle 1! That's where I'm a developer.
>
> Here is what is actually new:
>
> The hard problem with "agentic loops" is not the loop. It is *restraint*. A loop that happily opens 200 PRs is worse than no loop. So radioactive-ralph ships as ten structurally distinct variants — not ten flags on one skill, ten separate tool allowlists, model-tiering policies, parallelism limits, and termination conditions. /green-ralph is the classic unlimited loop. /red-ralph fixes CI and exits. /blue-ralph is read-only with Write and Edit literally excluded. /joe-fixit-ralph bills you at the end. /world-breaker-ralph requires --confirm-burn-everything because every agent runs on Opus.
>
> It runs as a Claude Code plugin (one command) or a standalone Python daemon (one pip install). Built on the Claude API, the gh CLI, and a lot of opinions about what autonomy should feel like. Early testers have run it for 400+ cycles without intervention.
>
> Repo, install instructions, and the ten variants: https://github.com/jbcom/radioactive-ralph
>
> I am interested in hearing from people who have tried to build something like this and hit walls. The walls are the interesting part.

**Notes on the LinkedIn copy:**
- No emojis (per user's global rules)
- Strong opening ("I shipped X") — no "excited to announce"
- The Ralph Wiggum angle is front-and-center but framed as a feature, not a gimmick
- The "what is genuinely new" section explicitly calls out *restraint* as the real design problem — this is the line that differentiates it from every other "autonomous coding agent" post
- Call to action at the end invites a specific kind of comment (people who have hit walls) — better engagement than "what do you think"
- Length: ~230 words, within LinkedIn's sweet spot (long enough to earn the "see more" click, short enough to finish)

## Draft — Hacker News Show post

**Title:**
> Show HN: Radioactive-Ralph – 10 structurally distinct autonomous orchestrators for Claude Code

**First paragraph (the important one — HN readers bounce if the first line doesn't land):**
> Radioactive-ralph drives Claude Code across a portfolio of git repos continuously, without human intervention. It ships as a family of ten Claude Code skills — not ten flags on one skill, ten genuinely distinct operating modes with their own model tiering, tool allowlists, parallelism limits, and termination conditions. `/green-ralph` is the unlimited loop. `/red-ralph` fixes CI and exits. `/blue-ralph` is read-only, with `Write` and `Edit` structurally excluded from the allowlist (not blocked by a prompt — actually absent from the tool set). `/joe-fixit-ralph` runs N cycles on a single repo and prints a "bill" at the end. There is also a standalone Python daemon mode that runs outside any Claude session, survives context resets, and spawns `claude --print` subprocesses. The whole thing logs in the voice of Ralph Wiggum from The Simpsons, which started as a joke and turned out to be the feature that made running 400 cycles in a row feel sane.

**Notes on the HN copy:**
- Title lead with the project name + the specific count (10) — HN scans titles for *concreteness*
- First paragraph does three things: (1) says what it *does*, (2) says what is *new* (structural not prompt-based constraint), (3) earns the weird tonal choice with a real reason
- The "not blocked by a prompt — actually absent from the tool set" line is the sentence HN will quote in comments

### Expected HN objections (pre-written replies)

1. **"Why not just use one skill with flags?"** Because autonomy is a safety problem, and safety wants structure, not configuration. `/blue-ralph` cannot accidentally edit a file because `Write` and `Edit` are not in its allowlist at all. A flags-based version would have to enforce that at runtime and get it right every time. The plugin manifest is the enforcement point.

2. **"Isn't this just a wrapper around `claude --print`?"** The daemon is a wrapper around the CLI, yes. The skills are not — they run *inside* a Claude Code session and use Claude Code's own tool invocation directly. Two layers, two different value props: the skills give you structured autonomy inside one session, the daemon gives you persistence across sessions.

3. **"Why Ralph Wiggum?"** Because the cost of running an autonomous loop for hours is watching it. A loop that is funny to watch is a loop you will actually watch, and a loop you watch is a loop that does not silently burn your API budget. The personality is load-bearing.

## Timing

- **Day 0:** Assets (social preview + demo GIF + variant icons) finalized and pushed
- **Day 0:** Fresh-machine install test on macOS and Ubuntu (`scripts/install.sh`)
- **Day 0:** GitHub social preview uploaded via repo settings
- **Day 1 morning (PT):** LinkedIn post
- **Day 1 afternoon (PT, ~11am PT ideal for HN front page):** HN Show submission
- **Day 1 evening:** Reply to comments for 2–3 hours live

## After launch

- Watch `gh issue list --state open` for bug reports
- Pin a "Known issues / roadmap" issue with the three most common reported problems
- First-week goal: one merged PR from an outside contributor
