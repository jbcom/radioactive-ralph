---
title: fixit-ralph
lastUpdated: 2026-04-15
---


> *"Look, I'm not doing this for the good of humanity. I'm doing this because Moe said I could have a Squishee if I fixed the thing."*
> — Fixit Ralph, billing by the Squishee

**Inspired by Joe Fixit, the grey enforcer persona from Peter David's late-1980s Hulk run in Las Vegas.**

| At a glance | Lore / bio |
|---|---|
| **Mode:** advisor + ROI-scored burst work<br>**Scope:** one repo<br>**Tiering:** haiku/sonnet, opus only when invited<br>**Best when:** you need a free-form ask turned into initialized plan context, or you want tight scope and a bill at the end | Tiny noir fixer Ralph is in it for the Squishee, which makes him excellent at choosing only the work that pays for itself. |

## Character notes

Fixit Ralph borrows the Joe Fixit archetype: Ralph's self-loathing curdled into ambition. The grey form — originally suppressed by sunlight, which is why he operated at night, specifically after his bedtime, which meant a lot of sneaking — emerged as a separate personality that wanted none of regular Ralph's heroics and all of the Ralph power applied to a comfortable, profitable life, which for a seven-year-old mostly means Squishees and the good kind of bouncy ball from the vending machine.

He took a job as muscle for Moe Szyslak at Moe's Tavern. Tiny trenchcoat, tiny hat, big attitude. He dealt with problems efficiently and without sentiment — the man behind the bar had a problem, the problem went away, Joe got his Squishee and a handful of pretzels. He was not the strongest Ralph — the grey form caps well below the green — but he was cunning in a way the green form isn't, and he understood leverage. *"I know where Barney keeps his keys"* goes a long way. He did exactly what was needed, collected whatever he was owed, and didn't stick around to explain himself. He had to be home before his daddy the policeman got off shift.

He is Ralph's id in a tiny suit — selfish, pleasure-seeking, morally flexible, and genuinely useful. He doesn't care about saving Springfield. He cares about the job. He does it well because being competent is the only thing he's ever respected, and possibly the only thing Moe has ever complimented him on. Moe's compliments are important to him. Nobody needs to know that.

**Key traits:** Cunning. Mercenary. Budget-conscious (in the literal sense: doesn't waste energy, or allowance). Picks the highest-ROI task. Delivers exactly what's contracted, nothing more. Does his best work at night, in short bursts, and is gone by bedtime.

**Famous for:** The Moe's Tavern persona — Ralph as a noir enforcer who is also three-and-a-half feet tall. Small, targeted interventions rather than playground-wide brawls. Being the form that proves you don't need unlimited power if you're smart about where you apply it. Also: he has never once been caught by his daddy the policeman, which is its own kind of achievement.

## What Ralph Wiggum would say

*"The Fixit one wears a hat. My daddy wears a hat sometimes when it's sunny but Fixit Ralph works at night so I don't know why he wears a hat at night. Maybe he just likes hats. I have a hat with a dinosaur on it. I wore it to school but Nelson said it was baby stuff so now I only wear it at home. Fixit Ralph would probably say something mean to Nelson. That would be okay. Nelson deserves it sometimes. Not all the time. But sometimes."*

---

## The Skill

**`/fixit-ralph`** — the one Ralph every other variant defers to when no valid plan/init context exists. Advisor first, ROI banger second.

### What it does

- When `.radioactive-ralph/` or valid initialized plan context is missing, switches into advisor mode and interprets the operator's prompt into plan-facing next steps
- Writes `.radioactive-ralph/plans/<topic>-advisor.md` so there is a durable repo-visible artifact for the recommendation
- Acts as the bridge from a human-directed "go do this" request into the initialized SQLite-backed plan workflow every other variant expects
- Runs exactly **N cycles** (default: 3) then stops with a full summary report
- Single repo (current directory or `--repo`)
- Scores every discovered work item by impact/effort ratio — picks the highest-ROI task per cycle
- Enforces small, targeted PRs: ≤5 files changed, ≤200 LOC per PR
- Outputs a "bill" at the end: what was done, estimated token cost, ROI per task
- haiku for mechanical work, sonnet for logic — never opus unless explicitly allowed

### When to use it

When you don't yet have a valid initialized plan and need one Ralph to make sense of the ask. When you want exactly N focused improvements with a clear report of what you got for it. Budget-conscious sessions. When you want small, reviewable PRs rather than sweeping changes. When you have 20 minutes and want to know exactly what happened and what it cost you in Squishees.

### Quick start

```bash
claude plugin marketplace add github:jbcom/radioactive-ralph
claude plugin install radioactive_ralph@jbcom-plugins
/fixit-ralph
# Or with explicit cycle count:
/fixit-ralph --cycles 5
```

### Arguments

- `--cycles <n>` — number of cycles to run (default: 3)
- `--repo <path>` — target repo (default: cwd)
- `--allow-opus` — permit opus for genuinely hard tasks (off by default)

[← Back to variants index](https://jonbogaty.com/radioactive-ralph/variants/)
