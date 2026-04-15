---
title: blue-ralph
updated: 2026-04-10
status: current
domain: product
---


> *"With my eyes, and not my hands. With my eyes, and not my hands. With my eyes, and not my hands."*
> — Blue Ralph, reciting his promise to Miss Hoover

**Inspired by the Captain Universe / Uni-Power Blue Hulk fusion from *Captain Universe: Incredible Hulk* #1 (2005), by Jay Faerber and Carlos Magno.**

| At a glance | Lore / bio |
|---|---|
| **Mode:** read-only observer<br>**Scope:** review and diagnosis<br>**Tiering:** sonnet only<br>**Best when:** you want the truth without any risk of modification | The cosmic museum-trip Ralph sees everything, touches nothing, and turns non-interference into the whole point of the skill. |

## Character notes

There is no canonical Blue Ralph the way there's a Joe Fixit or a Professor Ralph. What exists is a single remarkable event: the Uni-Power — the Enigma Force, a cosmic entity that roams Springfield occasionally choosing a host during a crisis — chose Ralph during a very bad field trip to the museum. For that brief window, the Blue Ralph possessed cosmic awareness, the ability to float, and a Ralph's baseline power amplified by something bigger than Springfield itself.

And what did he do with it? He kept his promise. The one Miss Hoover made him repeat at every field trip. *"I will look with my eyes and not my hands."* He is, in that moment, possibly the most powerful Ralph that has ever existed — and he did not touch a single painting. Not one. The tour guide almost cried.

The Uni-Power grants perception beyond normal senses — seeing systems, seeing consequences, seeing what is actually broken versus what only appears broken. The Blue Ralph doesn't smash. The Blue Ralph *diagnoses*. He points at the problem with a very small finger and says, *"that one is sad."*

In radioactive-ralph's universe, we claim this archetype: the blue variant is the Ralph who has been touched by something that sees everything, and uses that sight purely. No writing, no merging, no PRs. Only the most honest and useful thing a little boy can do: tell you exactly what's wrong without touching it.

**Key traits:** Cosmic awareness. Analytical precision. Complete non-interference. Museum-trained. The rarest and most temporary form — it doesn't last, because the Uni-Power always moves on (usually after the field trip is over).

**Famous for:** Being the one time Ralph kept his hands to himself for an entire day. The tour guide wrote it down. Miss Hoover framed the note. It is still on her refrigerator.

## What Ralph Wiggum would say

*"The blue one can see everything but he doesn't touch anything. My mom says I have to look with my eyes and not my hands but I don't always remember. The blue Ralph always remembers. I think that must be lonely. I touched a painting at the museum and a guard came over and I had to stand by my daddy the policeman for the rest of the visit. The blue Ralph would not have touched the painting. I want to be the blue Ralph sometimes but my hands do what they want. My hands are like a puppy. I can't tell them what to do. I can only say sorry later."*

---

## The Skill

**`/blue-ralph`** — Read-only review mode. Structural enforcement: `Write` and `Edit` are excluded from allowed-tools, making non-interference a harness guarantee, not just a promise.

### What it does

- Scans all open PRs and posts detailed review comments
- Never approves, never merges, never opens PRs
- Flags issues with severity (blocking / warning / suggestion)
- Posts structured review comments via forge API
- sonnet for all reviews

### When to use it

When you want a comprehensive review pass across your repos before you act. When you want a second opinion without any risk of modification. When you want to know what's wrong before deciding what to do about it. When you need a Ralph that keeps his hands in his pockets.

### Quick start

```bash
ralph install-skill --variant blue-ralph
/blue-ralph
```

### Arguments

- `--config <path>` — alternate config
- `--repo <path>` — single repo mode

[← Back to variants index](https://jonbogaty.com/radioactive-ralph/variants/)
