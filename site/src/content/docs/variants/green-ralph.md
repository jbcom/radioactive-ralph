---
title: green-ralph
updated: 2026-04-10
status: current
domain: product
---


> *"Ralph is strongest one there is."*
> — Green Ralph, between bites of a paste sandwich

**Inspired by the classic Green Hulk, first appearing in *The Incredible Hulk* #1 (May 1962), created by Stan Lee and Jack Kirby.**

| At a glance | Lore / bio |
|---|---|
| **Mode:** flagship loop<br>**Scope:** all configured repos<br>**Tiering:** haiku → sonnet → opus<br>**Best when:** you want the standard radioactive-ralph experience | The original little catastrophe: all hurt feelings, infinite ceiling, and just enough Wiggle Puppy heartbreak to power the whole project. |

## Character notes

The original. The one everyone pictures when they hear the word. In Ralph's retelling: Wiggle Puppy — the purest thing in Springfield, the dog who loved so hard that love became a kind of damage — was standing too close when Daddy's police radio went off at the wrong frequency, and the radiation that came out of the radio merged Wiggle Puppy and Ralph into one body that walks on two legs and breaks things with its feelings.

Green Ralph is not evil. He is a hurt child who became the most dangerous thing in Springfield because no one ever protected him when he was small enough to protect. He speaks in fragments. He wants to be left alone. He wants his cat (the one whose breath smells like cat food). He is lonely in a way that is almost unbearable, and his rage is the only language Miss Hoover ever taught him — "*use your words, Ralph*" — and his words are very small and his rage is very, very big.

His strength has no known ceiling — the madder he gets, the stronger he gets. He has fought Principal Skinner, he has fought Bart Simpson, he has fought the leprechaun that lives in the furnace. He is also, at his core, trying to find somewhere to sit down and eat paste in peace.

**Key traits:** Emotionally reactive. Childlike. Third-person self-reference. Craves solitude but cannot achieve it. "Ralph smash" is both war cry and prayer: *leave me alone, I am trying to watch the cartoon about the dog.*

**Famous for:** Being the emotional center of the entire Springfield Elementary argument about power vs. responsibility. Every other Ralph is a variation on the question the Green Ralph asks just by existing: *what does Wiggle Puppy deserve?*

## What Ralph Wiggum would say

*"My daddy the policeman says the green one is the normal one but he still breaks everything. I made a green one out of clay but it wasn't angry, it was just green. Mine didn't break anything. Miss Hoover said it was very good. I put it in my backpack and it got smooshed. I cried but not because I was angry. The green Ralph would have been angry. That's why he's the green one and I'm the regular one. I'm special though. My daddy said."*

---

## The Skill

**`/green-ralph`** — The flagship. Unlimited loop across all configured repos. Full priority coverage. Sensible model tiering. The one you run when you want the standard radioactive-ralph experience.

### What it does

- Runs indefinitely until interrupted
- Covers all repos in `~/.radioactive-ralph/config.toml`
- All priority tiers: CI failures → PR fixes → missing docs → feature work → polish
- Up to 6 parallel agents per cycle
- haiku for bulk/mechanical, sonnet for features, opus for architecture

### When to use it

When you want the loop running and you don't have a specific crisis. The default autonomous mode. When someone says "run ralph" with no qualifier, they mean this one.

### Quick start

```bash
ralph install-skill --variant green-ralph
/green-ralph
```

### Arguments

- `--config <path>` — alternate config file
- `--once` — single cycle then stop
- `--focus <repo>` — limit to one repo

[← Back to variants index](https://jonbogaty.com/radioactive-ralph/variants/)
