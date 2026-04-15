---
title: red-ralph
updated: 2026-04-10
status: current
domain: product
---


> *"I spent thirty years trying to contain Ralph. Now I am Ralph. The irony isn't lost on me."*
> — Red Ralph (Principal Seymour Skinner)

**Inspired by Red Hulk, first appearing in *Hulk* vol. 2 #1 (2008), with the Ross reveal landing during *World War Hulks*.**

| At a glance | Lore / bio |
|---|---|
| **Mode:** incident response<br>**Scope:** high-priority CI and PR blockers<br>**Tiering:** sonnet with opus escalation<br>**Best when:** something is on fire and you want one clean report | Skinner-as-Ralph is focused heat instead of chaos: a principal's clipboard welded onto a battlefield temperament. |

## Character notes

Principal Seymour Skinner spent his entire career trying to keep Ralph under control — the boy his school couldn't contain, the liability the district couldn't explain to parents, the kid who drew on the walls and ate the walls and then asked Miss Hoover if the walls were dead now. Then one day, alone in his office after a very bad parent-teacher conference, Principal Skinner was exposed to the same police radio radiation that made Ralph what he is — and he became a Ralph himself. A red one. The color of an angry detention slip.

Red Ralph is not emotional chaos. He is **controlled aggression** — a weapon that thinks. Where Green Ralph's fury is a tantrum, Skinner's Ralph is a school assembly: pre-planned, loud, and ending exactly on schedule. He assesses the hallway, identifies the problems in priority order (talkers first, then runners, then kids who haven't tucked in their shirts), and executes the cleanup. He doesn't get stronger with anger — he generates *heat*, body temperature rising to dangerous levels, exactly the way Principal Skinner's face does when Bart Simpson walks past his office.

He has defeated Bart Simpson. He has defeated the Abomination (who in this telling is Nelson Muntz with a bigger voice). He briefly held the Power Cosmic, which he used to hand out detention slips at the speed of light. He is burdened by the perfect irony of becoming what he hunted, and he carries that irony with principal bearing: tie straight, clipboard ready, zero self-pity, absolute focus on the objective.

**Key traits:** Tactical. Calculating. Bureaucratic. Controlled. Obsessive. Pride masquerading as professionalism. The irony does not break him — it *is* him now. He has a clipboard even in Ralph form.

**Famous for:** Detaining Nelson in his debut issue before his identity was even known. The reveal itself — thirty years of "Skinner hates Ralph" compressed into "yes, Skinner *is* a Ralph now and he's mad about it." Later: reluctant hero with the PTA, carrying the weight of what he did without excuses.

## What Ralph Wiggum would say

*"The red one used to be Principal Skinner but then he got turned into a Ralph like me and now he's mad that he's a Ralph but he's still a Ralph. My daddy says that's called ironing. I ironed a shirt once with my daddy and it was very hot and I had to stay back. The red Ralph is probably hot like that. I would stay back. Principal Skinner always stays back too. He has to, because of the restraining order. I don't know what a restraining order is but Miss Hoover said it."*

---

## The Skill

**`/red-ralph`** — Single cycle. High-priority only. Battlefield assessment and remediation. Reports like a principal to a school board.

### What it does

- Runs **one cycle** then stops with a structured report
- Covers only **CI_FAILURE** and **PR_FIXES** — nothing else
- sonnet for execution; escalates to opus if a problem is genuinely hard
- Outputs a structured "battlefield report" showing what was broken, what was fixed, what's still open

### When to use it

When something is on fire. CI failing. PR blocked on review feedback. You don't need the whole loop — you need the broken things fixed, now, with a clear report you can hand to your team lead the way Principal Skinner hands a report to Superintendent Chalmers.

### Quick start

```bash
ralph install-skill --variant red-ralph
/red-ralph
# Or scoped:
/red-ralph --repo ~/src/my-project
```

### Arguments

- `--repo <path>` — single repo mode (default: all configured repos)
- `--config <path>` — alternate config

[← Back to variants index](https://jonbogaty.com/radioactive-ralph/variants/)
