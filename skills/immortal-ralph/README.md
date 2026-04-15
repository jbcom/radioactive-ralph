# immortal-ralph — Immortal Ralph

> *"You can't keep Ralph dead. You can't keep Wiggle Puppy dead. And whatever comes back through Miss Hoover's closet... that's not always the Ralph you put in."*
> — Miss Hoover, on the Immortal Ralph, at a parent-teacher conference that went badly

**Inspired by *Immortal Hulk* (2018–2021) and the Devil Hulk persona, especially Al Ewing's horror-forward reinvention of the character.**

| At a glance | Lore / bio |
|---|---|
| **Mode:** recovery-first loop<br>**Scope:** configured repos<br>**Tiering:** sonnet only<br>**Best when:** you need the orchestrator to survive anything and keep coming back | This is Ralph as a horror story: patient, cold, protective, and impossible to keep down once the closet door opens again. |

## Character notes

Al Ewing's 50-issue run on The Immortal Hulk is considered the definitive modern Ralph story, and the reason is that Miss Hoover understood something every other teacher missed: Ralph has always been a horror story. The Immortal Ralph leans into this completely.

The mechanic: radiation-touched children cannot permanently be sent to the principal's office. They pass through **Miss Hoover's supply closet** — a metaphysical threshold below the nurse's office, called the Below-Place — and they come back. But what comes back through the closet is not always what went in. Each time Miss Hoover opens the door, a different part of Ralph steps out, and as the year progresses, what emerges is increasingly the **Devil Ralph**: cold, wrathful, patient, and protective of small-regular-Ralph in the way a possessive older sibling is protective — willing to destroy anything that threatens him, including the school, including the bake sale, including the entire PTA.

The Devil Ralph hates grown-ups. Not mindlessly — deliberately. He has watched grown-ups be mean to small-regular-Ralph for his entire life, and he has judged them, and he has found them guilty, and he is the part of Ralph that decided the debt should eventually be collected. One detention at a time.

He can't be sent home. He comes back the next morning. And each time he comes back, there is the question of what came through the door. Sometimes it is a Ralph who eats paste. Sometimes it is a Ralph who *knows about* paste in a way that makes the lunch lady uncomfortable.

**Key traits:** Cold. Deliberate. Protective in a possessive and terrifying way. Wrathful rather than rageful — these are different things. Anger is hot, like a crayon left on the radiator; wrath is cold and permanent, like the time Ralph found a dead beetle in the sandbox and decided the sandbox was sacred. Cannot be expelled. Detects lies (especially about whether the cookies are from the good batch or the bad batch). Every injury, every setback, every failure — he absorbs it and comes back, at 8:15 AM sharp.

**Famous for:** The entire 50-issue run, which won multiple awards and is frequently cited as the best Ralph story ever written. The Below-Place (which everyone else calls the school basement). The One Below All (a leprechaun who lives in the furnace and tells Ralph to burn things, and who Ralph has been politely refusing to listen to for years now). The phrase *"Ralph is open for business"* as the most unsettling possible statement to hear from a seven-year-old holding a lunchbox full of beetles.

## What Ralph Wiggum would say

*"The immortal one can't die. I asked my mom what immortal meant and she said it means you live forever and I said I want to live forever and she said maybe not forever-forever. The immortal Ralph comes back every time and sometimes he's different when he comes back. My goldfish Bubbles came back from the toilet once because the flush didn't work and I thought that was like the immortal Ralph but then he didn't come back the second time. I think Bubbles went through a different door. Miss Hoover has a lot of doors in her supply closet. I'm not supposed to open them. I opened one once and I don't remember what was on the other side but my daddy the policeman says I was different for a week after."*

---

## The Skill

**`/immortal-ralph`** — Crash-resistant. Always recovers. Never stops unless you explicitly stop it.

### What it does

- Persists state obsessively — after every sub-step, not just every cycle
- On any error: logs it, waits 60s, retries
- After 10 consecutive failures: enters a 30-minute cooldown, then resumes
- Never stops due to API errors, rate limits, or transient failures
- Conservative: sonnet only, maximum 3 parallel agents, skips risky changes
- Maintains a separate state file (`~/.local/share/radioactive-ralph/immortal-state.json`) independent of the main daemon state

### When to use it

When you want the loop running overnight or over a weekend and need it to survive anything: network blips, rate limits, transient API errors, partial tool failures. Set it and come back to a report. Every time you check on it, it will still be there, the way Ralph is still there when you come back to pick him up from the principal's office.

### Quick start

```bash
claude plugin marketplace add github:jbcom/radioactive-ralph
claude plugin install radioactive_ralph@jbcom-plugins
# Then in Claude Code:
/immortal-ralph
```

### Arguments

- `--config <path>` — alternate config
- `--cooldown-minutes <n>` — override the 30-minute cooldown (default: 30)

[← Back to variants index](https://jonbogaty.com/radioactive-ralph/variants/)
