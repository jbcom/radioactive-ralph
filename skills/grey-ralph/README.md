# grey-ralph — Grey Ralph

> *"I don't need to explain myself. You'll figure it out. Also I ate a worm."*
> — Grey Ralph

**Inspired by the original Grey Hulk from *The Incredible Hulk* #1 (May 1962), the first print-era form before the color settled on green.**

| At a glance | Lore / bio |
|---|---|
| **Mode:** mechanical janitor<br>**Scope:** one repo<br>**Tiering:** haiku only<br>**Best when:** you need governance docs and file hygiene without spending judgment | The first form is sly, practical, and paste-motivated: less heroism, more quietly getting the boring work done and leaving before anyone asks questions. |

## Character notes

The first Ralph was grey. Not green. The decision to go green was a printing accident, not an artistic choice — and the grey form was quietly buried until Miss Hoover dug it back up during a unit on "things that are true even if nobody remembers them." In Ralph's version: the very first time Wiggle Puppy loved him too hard, Ralph didn't turn green. He turned grey. Grey like the sweater his nana knitted him. Grey like a day in Springfield when the factory smoke is bad. Grey like the inside of his favorite crayon box after he ate all the other colors.

Where Green Ralph is a wounded child, Grey Ralph is something older and nastier: Ralph's suppressed selfishness and pragmatic amorality, cut loose. He is not driven by rage. He is driven by *want*. He wants paste. He wants to be left alone to eat the paste. He wants the good kind of paste that's slightly sweet, not the school kind that tastes like teacher. He is morally flexible in a way that makes him useful for exactly the kind of work that doesn't require judgment — only execution, and maybe a little paste.

He is weaker than the green form physically (capped around 70-100 paste jars baseline), but smarter — and he doesn't waste strength he doesn't need. He's the one who figured out you can just *walk* past the bullies instead of fighting them, if you do it while they're looking the other way.

**Key traits:** Cunning. Sarcastic. Mercenary. No guilt. No heroics. Just the job. Also paste.

**Famous for:** Being the one Ralph nobody remembers exists, even though he was there first. Operating at night, because the first grey form was suppressed by sunlight. He does his best work in the dark, which is also when the good paste lives.

## What Ralph Wiggum would say

*"The grey one doesn't get as mad as the green one. Miss Hoover says grey is what you get when you mix black and white together. I tried to mix black and white Play-Doh and I got grey but then I kept mixing it and it turned brown. The grey Ralph probably doesn't like being mixed with other colors. He seems like he wouldn't like that. I wouldn't like that either. One time somebody mixed my apple juice with my milk and I cried for most of snack time."*

---

## The Skill

**`/grey-ralph`** — The mechanical workhorse. Single repo. Haiku only. File hygiene only. No judgment required.

### What it does

- Works only on the current repo (or `--repo <path>`)
- Uses **haiku for everything** — no sonnet, no opus
- Handles only safe, mechanical work: missing governance files, frontmatter, CHANGELOG entries, stub docs
- Never writes or rewrites source code
- Does one thing, opens one PR, exits

### When to use it

When you have a repo that needs its governance files sorted out and you don't want to spend tokens thinking about it. Send in grey-ralph, let him do the rote work, move on. He will eat some paste on the way out but he won't touch your source code.

### Quick start

```bash
claude plugin marketplace add github:jbcom/radioactive-ralph
claude plugin install radioactive_ralph@jbcom-plugins
# Then in Claude Code:
/grey-ralph
# Or scoped:
/grey-ralph --repo ~/src/my-project
```

### Arguments

- `--repo <path>` — target repo (default: cwd)

[← Back to variants index](https://jonbogaty.com/radioactive-ralph/variants/)
