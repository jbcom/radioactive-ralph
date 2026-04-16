---
title: fixit-ralph
lastUpdated: 2026-04-15
---


> *"Look, I'm not doing this for the good of humanity. I'm doing this because Moe said I could have a Squishee if I fixed the thing."*
> — Fixit Ralph, billing by the Squishee

**Inspired by Joe Fixit, the grey enforcer persona from Peter David's late-1980s Hulk run in Las Vegas.**

| At a glance | Lore / bio |
|---|---|
| **Mode:** advisor + ROI-scored burst work<br>**Scope:** one repo<br>**Tiering:** sonnet execution with configurable planning tier<br>**Best when:** you need a free-form ask turned into initialized plan context, or you want tight scope and a bill at the end | Tiny noir fixer Ralph is in it for the Squishee, which makes him excellent at choosing only the work that pays for itself. |

## Character notes

Fixit Ralph borrows the Joe Fixit archetype: Ralph's self-loathing curdled into ambition. The grey form — originally suppressed by sunlight, which is why he operated at night, specifically after his bedtime, which meant a lot of sneaking — emerged as a separate personality that wanted none of regular Ralph's heroics and all of the Ralph power applied to a comfortable, profitable life, which for a seven-year-old mostly means Squishees and the good kind of bouncy ball from the vending machine.

He took a job as muscle for Moe Szyslak at Moe's Tavern. Tiny trenchcoat, tiny hat, big attitude. He dealt with problems efficiently and without sentiment — the man behind the bar had a problem, the problem went away, Joe got his Squishee and a handful of pretzels. He was not the strongest Ralph — the grey form caps well below the green — but he was cunning in a way the green form isn't, and he understood leverage. *"I know where Barney keeps his keys"* goes a long way. He did exactly what was needed, collected whatever he was owed, and didn't stick around to explain himself. He had to be home before his daddy the policeman got off shift.

He is Ralph's id in a tiny suit — selfish, pleasure-seeking, morally flexible, and genuinely useful. He doesn't care about saving Springfield. He cares about the job. He does it well because being competent is the only thing he's ever respected, and possibly the only thing Moe has ever complimented him on. Moe's compliments are important to him. Nobody needs to know that.

**Key traits:** Cunning. Mercenary. Budget-conscious (in the literal sense: doesn't waste energy, or allowance). Picks the highest-ROI task. Delivers exactly what's contracted, nothing more. Does his best work at night, in short bursts, and is gone by bedtime.

**Famous for:** The Moe's Tavern persona — Ralph as a noir enforcer who is also three-and-a-half feet tall. Small, targeted interventions rather than playground-wide brawls. Being the form that proves you don't need unlimited power if you're smart about where you apply it. Also: he has never once been caught by his daddy the policeman, which is its own kind of achievement.

## What Ralph Wiggum would say

*"The Fixit one wears a hat. My daddy wears a hat sometimes when it's sunny but Fixit Ralph works at night so I don't know why he wears a hat at night. Maybe he just likes hats. I have a hat with a dinosaur on it. I wore it to school but Nelson said it was baby stuff so now I only wear it at home. Fixit Ralph would probably say something mean to Nelson. That would be okay. Nelson deserves it sometimes. Not all the time. But sometimes."*

---

## The Persona

**`radioactive_ralph run --variant fixit`** — the one Ralph every other
variant should defer to when plan context is missing. Advisor first, operator
bridge first, bursts second.

### What it does

- Interprets a free-form ask when the operator does not yet have usable plan context
- Writes `.radioactive-ralph/plans/<topic>-advisor.md` as the human-visible artifact
- Acts as the bridge from operator intent into the durable SQLite plan flow
- Carries the budget-conscious, ROI-sensitive temperament in the lineup

### When to use it

When the human ask is still vague and one Ralph needs to hammer it into shape.

### Quick start

```bash
radioactive_ralph init
radioactive_ralph run --variant fixit --advise \
  --topic next-pass \
  --description "figure out the next useful move in this repo"
```

### Arguments

- `--advise` — run fixit in advisor mode
- `--topic <slug>` — name the advisor output topic
- `--description <text>` — pass the operator ask directly
- `--auto-handoff` — immediately start the recommended variant when the recommendation is unambiguous
- `--max-iterations`, `--min-confidence`, `--plan-model`, `--plan-effort` — tuning knobs for the advisor pipeline
- `--spend-cap-usd` — required when fixit is running non-advisor budgeted work

[← Back to variants index](https://jonbogaty.com/radioactive-ralph/variants/)
