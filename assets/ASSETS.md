---
title: Launch Assets Manifest
updated: 2026-04-10
status: current
domain: creative
---

# Launch Assets Manifest

Everything the repo needs to look polished on launch day (LinkedIn + Hacker News drop). Each entry says what the asset is, where it lives, how to produce it, and its current status.

Binary assets (PNG, GIF, SVG) are produced by the scripts in `scripts/` or by hand — this file is the source of truth for *what needs to exist* and *what it should contain*. Do not delete entries after shipping; flip status to `DONE` so the checklist in [`docs/guides/launch.md`](../docs/guides/launch.md) can reference them.

---

## 1. Hero image — `assets/brand/ralph-mascot.png`

**Status:** DONE

**What it is:** The primary mascot. A green, radioactive Ralph Wiggum — the visual anchor for the whole project. Used at the top of the main README (rendered 400px wide, centered) and referenced by docs.

**Dimensions:** Source is roughly 2.4MB PNG at native resolution. Displays well at 300–500px.

**Where it's used:**
- `README.md` header (via the raw GitHub URL for `assets/brand/ralph-mascot.png`, so PyPI renders it correctly)
- Referenced by docs/guides/design.md
- Should NOT be re-used as the GitHub social preview — that needs its own composition (see §2)

---

## 2. Social preview — `assets/social-preview.png`

**Status:** TODO

**What it is:** A 1280×640 PNG built for GitHub's Open Graph card (the image that appears when you share the repo link on LinkedIn, Twitter/X, Slack, HN, etc.). GitHub requires exactly this aspect ratio and will letterbox anything else.

**What it should contain:**
- Left third: the `ralph-mascot.png` (bleed off the bottom slightly for energy)
- Middle: large title `radioactive-ralph` in the project's green (`#22c55e` / Rich `bright_green` equivalent)
- Subtitle directly below: `autonomous continuous development orchestrator for Claude Code`
- Right third: a 2×5 grid of the ten variant icons (see §4) in their variant colors, labeled with the short name (`green`, `grey`, `red`, `blue`, `professor`, `savage`, `immortal`, `fixit`, `old-man`, `world-breaker`)
- Bottom-right corner tagline in small type: *"Ralph has many forms."*
- Dark background (`#0a0a0a`) so the green and the mascot pop

**How to produce it:** Hand-composed in Figma / Affinity / Sketch once the variant icons (§4) exist. Export 1280×640 PNG, optimize with `oxipng -o 4 assets/social-preview.png`.

**Where it's used:** Upload via GitHub repo Settings → Social preview (documented in the main README's "Launch notes" collapsible block).

---

## 3. Demo GIF — `assets/demo.gif`

**Status:** TODO

**What it is:** A ~30-second terminal recording showing radioactive-ralph in action. Goal is to convey: *Ralph is alive, Ralph is funny, Ralph does actual work.* Not a tutorial — a vibe demo.

**Shot list (what the viewer must see, in order):**
1. Empty terminal, prompt visible
2. `radioactive_ralph status` — shows cycle count, active runs, recent Ralph events (from `_RECENT_EVENTS`)
3. Pause 1s
4. `ralph discover` — shows discovered work items colored per priority tier, with Ralph narration
5. Pause 1s
6. `ralph pr list` — shows the classified PR list across configured repos
7. Pause 1s
8. One Ralph Wiggum quote visible throughout (the narration is the joke)
9. Final frame lingers ~2s on a fully populated status output so the GIF's last frame reads as a poster

**How to produce it:** Use [vhs](https://github.com/charmbracelet/vhs) with the tape file at `scripts/demo.tape`. That file is the source of truth — any visual changes to the GIF should flow through the tape, not through recording freehand. Run `scripts/record-demo.sh` and it will detect vhs, asciinema+agg, or print instructions.

**Dimensions:** 1200×720 window size set by the tape. Target output ~1–3 MB GIF (if larger, re-encode with `gifsicle -O3`).

---

## 4. Per-variant icon set — `assets/variants/*.svg`

**Status:** TODO

**What it is:** Ten small SVG icons, one per variant. Used in the social preview (§2), the skills index page, and the per-variant READMEs (optional, future).

**Common visual vocabulary:**
- 128×128 viewBox
- Ralph silhouette (simplified — just the head is fine, hair tuft + closed eyes)
- Each icon uses its variant's color scheme from `_COLORS` in `src/radioactive_ralph/ralph_says.py`
- Consistent stroke weight (4px), rounded caps
- Transparent background

**Per-variant colors (from `_COLORS`):**

| File | Variant | Primary | Accent | Warn | Visual motif |
|------|---------|---------|--------|------|--------------|
| `green-ralph.svg` | GREEN | `green` | `bright_green` | `yellow` | Classic — the base silhouette, nothing extra |
| `grey-ralph.svg` | GREY | `white` | `bright_white` | `yellow` | Holding a broom (file hygiene) |
| `red-ralph.svg` | RED | `red` | `bright_red` | `orange3` | Siren/megaphone (CI failures) |
| `blue-ralph.svg` | BLUE | `blue` | `bright_blue` | `cyan` | Spectacles / magnifying glass (observer) |
| `professor-ralph.svg` | PROFESSOR | `magenta` | `bright_magenta` | `yellow` | Mortarboard + tiny scroll |
| `savage-ralph.svg` | SAVAGE | `bright_green` | `green` | `red` | Eyes glowing, mouth open (mindless) |
| `immortal-ralph.svg` | IMMORTAL | `dark_green` | `green4` | `red3` | Phoenix wings behind silhouette |
| `fixit-ralph.svg` | JOE_FIXIT | `grey62` | `grey82` | `yellow3` | Fedora + cigar (noir fixer) |
| `old-man-ralph.svg` | OLD_MAN | `dark_red` | `red3` | `bright_red` | Iron crown (the Maestro) |
| `world-breaker-ralph.svg` | WORLD_BREAKER | `bright_red` | `red` | `bright_white` | Cracked-earth base under silhouette |

**How to produce them:** Hand-authored SVGs, or generate a base template and swap the `stroke`/`fill` attributes per variant. Keep each file < 4KB.

---

## 5. Architecture diagram — `assets/architecture.svg`

**Status:** TODO

**What it is:** A single SVG that replaces the ASCII diagram in `docs/reference/architecture.md` for places where SVG renders better (GitHub Pages, blog posts, the social preview if we want a variant).

**What it shows:**
- `ralph` daemon (the two-layer process) on the left
- Arrow pointing right to `claude` CLI subprocesses (stacked, showing up to N parallel)
- Each Claude subprocess arrow-pointing to a repo
- Each repo arrow-pointing back to `gh` / GitHub (PRs)
- Feedback loop arrow from GitHub back to the daemon (the `forge-client` / `pr_manager` layer)
- Eight-phase cycle labeled around the daemon: `ORIENT → DRAIN_MERGE_QUEUE → INTERNAL_REVIEW → ADDRESS_FEEDBACK → DISCOVER_WORK → SPAWN_AGENTS → HANDLE_COMPLETIONS → SLEEP`
- Color-code: daemon in green, Claude subprocesses in magenta (opus/sonnet/haiku tiers), GitHub in grey

**How to produce it:** Author in [Excalidraw](https://excalidraw.com) or [tldraw](https://tldraw.com), export SVG, strip metadata with `svgo` or `scour`. Must render correctly on both light and dark GitHub themes — avoid pure black/white fills, use semi-transparent strokes.

---

## Production order

1. Per-variant icons (§4) — blocks the social preview
2. Architecture diagram (§5) — independent
3. Social preview (§2) — depends on §4
4. Demo GIF (§3) — independent, but do it last so any CLI output tweaks are captured

## Optimization commands

```bash
# PNG
oxipng -o 4 assets/social-preview.png
oxipng -o 4 assets/brand/ralph-mascot.png

# SVG
svgo assets/variants/*.svg assets/architecture.svg

# GIF
gifsicle -O3 --colors 128 assets/demo.gif -o assets/demo.gif
```
