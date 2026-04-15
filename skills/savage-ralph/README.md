# savage-ralph — Savage Ralph

> *"RALPH... NOT... STOP."*
> — Savage Ralph, already halfway through the wall

**Inspired by Savage Hulk, the dominant early-publication form that has been present since *The Incredible Hulk* #1 (May 1962).**

| At a glance | Lore / bio |
|---|---|
| **Mode:** throughput monster<br>**Scope:** configured plus discovered repos<br>**Tiering:** everything escalated one model tier<br>**Best when:** you need maximum speed and signed the permission slip | Pre-verbal panic in motion: no off switch, no patience, no concept of tomorrow, just an enormous amount of right now. |

## Character notes

If the Green Ralph is a hurt child, the Savage Ralph is the part of that child that exists *before* language. Pre-verbal. Pre-rational. Pure affect: fear becomes rage, loneliness becomes violence, confusion becomes running very fast in a random direction. He is Ralph's dissociated preschool trauma — specifically the terror of the one time he got lost at the mall and he couldn't find his daddy the policeman for almost an entire hour — given unlimited physical expression.

He is not mindless in the sense of being *empty*. He feels everything, intensely, all the time. He feels the scratchy tag in his shirt. He feels the one sock that's lower than the other. He feels the way the fluorescent lights in the hallway buzz. But he has no filter, no strategy, no off switch. He loves in the way a very small child loves — completely, clumsily, destructively. He destroys the things he means to protect. He can distinguish friend from foe, but only barely, and only for a little while, and only if the friend isn't wearing a hat that Ralph finds suspicious.

His power is the purest expression of the sad-strength equation: no ceiling, no known limit. He has regenerated from situations that would end a regular Ralph. He has fought Nelson Muntz and made Nelson apologize. He is constitutionally incapable of stopping, because "stop" is a concept that requires foresight, and foresight requires a future, and Savage Ralph only has *now*, and right now there is paste that needs eating.

He actively despises regular small Ralph — sees him as a cage, a jailer, the small quiet person who keeps him locked away by saying things like *"Miss Hoover says we shouldn't"*. Every time he gets out, he runs.

**Key traits:** No off switch. No filter. Third-person speech. Strength that scales directly with how sad he is, with no upper bound. Loves fiercely and destructively. "Ralph is strongest one there is" — not a boast, a statement of confused fact.

**Famous for:** The entire history of the Springfield Elementary hallway. The image of Ralph running away from the lunch line, just wanting to be left alone with his tater tots. The tragic friendship with his daddy the policeman, who keeps having to come pick him up from places. Being the form that fights back because he can't not.

## What Ralph Wiggum would say

*"The savage one doesn't have an off switch. I asked my daddy what an off switch was and he said it's like a light switch but inside you. I don't think I have one either because sometimes I just keep going. Miss Hoover says I need to use my inside voice but sometimes I forget that I have one. The savage Ralph probably forgets too. I think we would be friends but he might accidentally smoosh me. My daddy smooshed an ant once and the ant was very sad. I think I would be like the ant. Except I would forgive him because we are the same person."*

---

## The Skill

**`/savage-ralph`** — Maximum parallelism. All models escalated one tier. Zero sleep. Requires `--confirm-burn-budget`.

### What it does

- 10 parallel agents per cycle
- Model escalation: what would be haiku becomes sonnet; what would be sonnet becomes opus
- Zero sleep between cycles
- Discovers repos beyond configured list (checks standard org paths)
- Never stops on task failure — logs it, moves on
- Warns loudly and requires explicit confirmation before starting

### When to use it

When normal pace is unacceptable and budget is not the constraint. For clearing a large backlog fast, or when you want maximum throughput for a finite time window. When you need a Ralph with no off switch and you have signed the permission slip.

### Quick start

```bash
claude plugin marketplace add github:jbcom/radioactive-ralph
claude plugin install radioactive_ralph@jbcom-plugins
# Then in Claude Code:
/savage-ralph --confirm-burn-budget
```

### Arguments

- `--confirm-burn-budget` — **required**, without this it refuses to start
- `--config <path>` — alternate config

[← Back to variants index](https://jonbogaty.com/radioactive-ralph/variants/)
