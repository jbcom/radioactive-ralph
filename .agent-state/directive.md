# radioactive-ralph — supervisor-architecture rewrite directive

**Status:** ACTIVE — never flips to RELEASED without an explicit manual "end the
session" from the user (directive 2026-07-17). The loop below keeps generating
work; the queue never legitimately drains.

## Directive 0 — the perpetual-shipping loop (ALWAYS ACTIVE, never checked off)

Standing mandate (user, 2026-07-17): "block on nothing. I want YOU making all
calls and working autonomously... always forward exploring with different review
skills and finding new polish and features that make this a better product until
I manually end the session. Until then keep mutating your loop and scheduled
reminder and directives so you're always shipping new PRs."

**This item is NEVER done and NEVER `[x]`.** It is the engine that keeps the
queue non-empty. Each pass:

1. **Ship the current concrete item** (the checklist below) to a merged PR —
   build → self-review trio (code/security/simplify) → PR → babysit CI + bots →
   squash-merge. Full autonomy: open PRs, resolve threads, and self-merge green
   PRs without asking. Design/architecture/packaging/naming calls are MINE; an
   AskUserQuestion for an impl/design detail under this mandate is a defect.
2. **When the concrete checklist is empty, GENERATE the next item** by
   forward-exploring — rotate through these lenses and append what they find as
   fresh `[ ]` items under "Rolling improvement queue":
   - comprehensive-review / feature-dev:code-reviewer on the newest merged code
   - security-scanning:security-sast on any new surface
   - code-simplifier on the largest / newest files
   - UI/UX pass on the TUI + GUI (run them, screenshot, compare to intent)
   - a NEW feature or polish that makes Ralph a better product (GUI richness,
     provider coverage, observability, DX, docs, perf)
   - dependency freshness + CVE sweep
3. **Compress finished work.** When a phase/effort fully lands, move its detail
   OUT of this file into the pillar doc `docs/superpowers/PILLARS.md` (one
   tight paragraph per shipped effort with the merge SHA + PR#), and leave only a
   one-line pointer here. Keeps the directive short and scannable.
4. **Keep the loop alive.** Re-arm ScheduleWakeup every tick; mutate cadence and
   the concrete queue as the work demands. Only a true blocker (interactive
   credential entry, a spend needing payment auth, physical hardware, or
   remote-state-I-already-triggered) is a legitimate `[WAIT-*]` yield — and even
   those route to OTHER queued work rather than halting. The user's SignPath
   enrollment is optional and NOT a blocker: everything ships unsigned-but-
   working without it.

Only the user typing an explicit end ("end the session", "stop the loop", "we're
done") flips Status→RELEASED and stops directive 0. Nothing else does.

---

**Original rewrite status (historical):** rewrite merged (v0.10.0). "Done" = every gap we identified + everything comprehensive-review / UI-review / security / simplification / bug-hunt digging surfaces is resolved. Drove a full multi-lens audit of merged main, then the desktop-app + onboarding effort.

Orchestrator: this agent. Executors: chosen per-task (haiku=mechanical,
sonnet=standard impl, opus/fable=hard reasoning) via Workflow fan-outs.
Each task ends build/test-green (branch is mid-flight but every checkpoint
compiles + passes its own tests). One large branch; final PR(s) at the end.
Full decision trail: .agent-state/decisions.ndjson. Spec:
docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md.

## Shipped (compressed → docs/superpowers/PILLARS.md)

- Supervisor-architecture rewrite (v0.10.0) — PRs #73/#74/#75.
- Post-release multi-lens audit (→ v0.10.3, converged) — PRs #76/#79/#81/#83.
- Guided first-run onboarding — PR #85 (80daad9).
- Versioned IPC drive+observe API — PR #87 (2f20adf).
- Fyne desktop GUI client — PR #89 (e969551).

Detail lives in PILLARS.md; consult .agent-state/decisions.ndjson for the why
behind any load-bearing call.

## Concrete queue (current)

- [ ] [WAIT-REVIEW] Native installers & GUI packaging — PR #92 open (feat/native-packaging). goreleaser nfpms (.deb/.rpm) + winget + homebrew_casks (brews→casks migration for goreleaser v2.17); GUI-bundle release matrix (fyne package --tags gui per-OS → macOS .app+ad-hoc-codesign+.dmg+cask PR, Linux AppImage+.desktop, Windows .exe); FyneApp.toml + 512² icon; install.sh dead-model fix; packaging-lint CI job (goreleaser check + shellcheck + desktop-file-validate). macOS path proven locally on darwin; two AppImage runtime bugs + the v2.17 cask deprecation caught by review/CI and fixed. Spec: docs/superpowers/specs/2026-07-17-native-packaging-design.md. Blocked only on CI green → self squash-merge.

## Rolling improvement queue (directive 0 appends here)

_(empty — next tick's forward-exploration pass fills this once #92 merges: pick a
review lens or a polish/feature and append concrete `[ ]` items.)_

## Notes

- Model selection for subagents: haiku=mechanical, sonnet=standard, opus/fable=hard reasoning; reserve opus for <10%.
- Per-commit self-review trio (code/security/simplify) then fold findings forward; never amend a reviewed commit.
- CodeRabbit/bot rate-limit red check = false-flag; the signal is the review threads (resolve via GraphQL), not the check status.
