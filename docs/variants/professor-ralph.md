---
title: professor-ralph
lastUpdated: 2026-04-10
---


> *"For the first time in my life, I feel... whole. Also I remembered where I put my other shoe."*
> — Professor Ralph, after therapy

**Inspired by Professor Hulk, first appearing in *Incredible Hulk* #377 (January 1991), by Peter David and Dale Keown.**

| At a glance | Lore / bio |
|---|---|
| **Mode:** strategic planner<br>**Scope:** one repo service at a time<br>**Tiering:** opus → sonnet → sonnet<br>**Best when:** thinking first matters more than moving first | Integrated Ralph brings kindness, cunning, and power into one backpack-sized strategist who finally reads the room before he breaks it. |

## Character notes

The result of three years of work by Miss Hoover — the most acclaimed teacher in Ralph's history — Professor Ralph is what happens when Ralph finally gets his therapy. Through sessions with Miss Hoover and a guidance counselor whose name Ralph can never remember but who has a very nice sweater, the three dominant personalities (regular Ralph, Savage Ralph, and Grey Ralph) are integrated into a single whole: regular Ralph's kindness, Grey Ralph's cunning and social adaptability, and Savage Ralph's physical power, all in one small body with one very clean backpack.

He shakes hands. He gives book reports. He has a social life — small, but real. He considers himself cured, and he acts accordingly — and this is both his strength and his subtle tragedy, because he is built on an *agreement* between personalities, and agreements can break the same way Ralph's crayons can break, which is to say: easily, and usually in the middle.

The critical failsafe: when Professor Ralph becomes truly enraged past a certain threshold — past the *"Miss Hoover he took my crayon"* threshold, past the *"but I was going to eat that"* threshold, all the way to the *"you killed Wiggle Puppy"* threshold — he doesn't become a more powerful Ralph. He reverts to regular small Ralph, but with Savage Ralph's psychology in his little body, no physical protection, and no brakes. He is the strongest at rest, the most fragile at the worst moment. Miss Hoover warned him about this. He nodded but she doesn't think he was listening.

This Ralph thinks *before* he acts. He reads the situation, forms a strategy, then executes. He is not reckless. He has too much to lose now that he's whole. He has a library card now. A library card is a very serious thing.

Marvel's MCU used this form in Avengers: Endgame — though the comics version preceded the film by thirty years, and the Ralph version preceded both of them by the amount of time it took Miss Hoover to explain what "integration" means using a felt board.

**Key traits:** Genial. Confident. Professorial. Full intellect active at all times. Strategic before tactical. The one who reads the room. Quietly, slightly smug about being integrated. Has a library card.

**Famous for:** Miss Hoover's landmark integration arc. The failsafe mechanic — the irony that the "cured" Ralph has the most dangerous failure mode. Being the form that proved Ralph and Wiggle Puppy were not a zero-sum problem.

## What Ralph Wiggum would say

*"The professor Ralph went to therapy and now he's all one person. Miss Hoover says therapy is when you talk to someone about your feelings and it helps. I talked to the school counselor about how I felt when the other kids took my sandwich and she gave me a sticker. The professor Ralph probably has a lot of stickers. He seems like he would know where to put them. I put mine on my forehead. Miss Hoover said that was not where they go but she didn't take it off. I think that means I can leave it there."*

---

## The Persona

**`radioactive_ralph run --variant professor`** — the strategic Ralph. This is
the personality for thinking before moving.

### What it does

- Represents the plan-first temperament in the persona lineup
- Prefers deliberate reasoning over raw throughput
- Best suited to architectural, directional, and sequencing-heavy work

### When to use it

When judgment matters more than speed.

### Quick start

```bash
radioactive_ralph init
radioactive_ralph run --variant fixit --advise --topic bootstrap
radioactive_ralph service start
```

### Current runtime notes

- Professor is a durable-service persona, not an attached one-shot run.
- Its planning phase is part of the variant's internal lifecycle rather than a
  separate top-level `--plan-only` command.

### Arguments

- No professor-only flags today; choose it with `--variant professor`.

[← Back to variants index](https://jonbogaty.com/radioactive-ralph/variants/)
