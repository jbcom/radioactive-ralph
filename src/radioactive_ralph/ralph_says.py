"""Ralph says things. Ralph says a lot of things.

Ralph Wiggum — son of Chief Clancy Wiggum, resident of Springfield,
student at Springfield Elementary, owner of a cat whose breath smells
like cat food — has opinions about everything your orchestrator is doing.

This module provides variant-aware Rich-formatted status messages that
replace sterile log output with something considerably more Ralph.

Each variant has its own personality, color scheme, and quote set.
Messages are selected randomly within category so Ralph stays surprising.

Example::

    from .ralph_says import ralph_says, Variant
    ralph_says(Variant.GREEN, "startup")
    ralph_says(Variant.OLD_MAN, "merge", pr_number=42, repo="kings-road")
"""

from __future__ import annotations

import random
from collections import deque
from datetime import UTC, datetime
from enum import Enum

from rich.console import Console
from rich.panel import Panel
from rich.text import Text

console = Console()

# Ring buffer of recent Ralph events for the live dashboard to read.
# Capped at 50 entries — oldest evicted automatically by deque(maxlen=).
# Entries are (Variant, rendered_markup, timestamp).
_RECENT_EVENTS: deque[tuple[Variant, str, datetime]] = deque(maxlen=50)


def recent_events() -> list[tuple[Variant, str, datetime]]:
    """Return a snapshot of recent Ralph events (oldest→newest)."""
    return list(_RECENT_EVENTS)


class Variant(str, Enum):
    """The many forms of Ralph."""
    GREEN = "green-ralph"
    GREY = "grey-ralph"
    RED = "red-ralph"
    BLUE = "blue-ralph"
    PROFESSOR = "professor-ralph"
    SAVAGE = "savage-ralph"
    IMMORTAL = "immortal-ralph"
    JOE_FIXIT = "joe-fixit-ralph"
    OLD_MAN = "old-man-ralph"
    WORLD_BREAKER = "world-breaker-ralph"


# ── Color themes per variant ────────────────────────────────────────────────

_COLORS: dict[Variant, dict[str, str]] = {
    Variant.GREEN:        {"primary": "green",       "accent": "bright_green",  "warn": "yellow"},
    Variant.GREY:         {"primary": "white",        "accent": "bright_white",  "warn": "yellow"},
    Variant.RED:          {"primary": "red",          "accent": "bright_red",    "warn": "orange3"},
    Variant.BLUE:         {"primary": "blue",         "accent": "bright_blue",   "warn": "cyan"},
    Variant.PROFESSOR:    {"primary": "magenta",      "accent": "bright_magenta","warn": "yellow"},
    Variant.SAVAGE:       {"primary": "bright_green", "accent": "green",         "warn": "red"},
    Variant.IMMORTAL:     {"primary": "dark_green",   "accent": "green4",        "warn": "red3"},
    Variant.JOE_FIXIT:    {"primary": "grey62",       "accent": "grey82",        "warn": "yellow3"},
    Variant.OLD_MAN:      {"primary": "dark_red",     "accent": "red3",          "warn": "bright_red"},
    Variant.WORLD_BREAKER:{"primary": "bright_red",   "accent": "red",           "warn": "bright_white"},
}

# ── Ralph's vocabulary, organized by what's happening ──────────────────────
#
# Keys: startup, cycle_start, scanning, merging, merged, reviewing, reviewed,
#       discovering, executing, agent_done, agent_failed, sleeping, shutdown,
#       error, warning, success, budget_warning
#
# Ralph speaks. Ralph always speaks. You cannot stop Ralph from speaking.

_QUOTES: dict[str, list[str]] = {
    "startup": [
        "I dressed myself! [bright_white]And now I'm running![/bright_white]",
        "Oh boy, sleep! That's where I'm a Viking! But first, [bright_white]I have to do the loop.[/bright_white]",
        "My daddy the policeman says I have to do a good job. [bright_white]I'll try my best.[/bright_white]",
        "I'm learnding! [bright_white]Well, actually I'm starting. But later I'll be learnding.[/bright_white]",
        "I'm special! [bright_white]The config file said so. Well, the config file has my repos in it, which is basically the same thing.[/bright_white]",
        "I found a moon rock in my nose! [bright_white]Also I found some repos. Let's do the repos.[/bright_white]",
        "Hi, Super Nintendo Chalmers! [bright_white]I mean, hi, developer. I'm starting now.[/bright_white]",
    ],
    "cycle_start": [
        "Cycle [bright_white]{cycle}[/bright_white]! I'm doing cycle [bright_white]{cycle}[/bright_white]! That's where I'm a [bright_white]developer![/bright_white]",
        "My cat's breath smells like cat food. [bright_white]Also it's cycle {cycle}.[/bright_white]",
        "I bent my wookie. [bright_white]But I'm still doing cycle {cycle}.[/bright_white]",
        "I ated the purple berries. [bright_white]Now: cycle {cycle}.[/bright_white] It tastes like... doing work.",
        "When I grow up I'm going to Bovine University! [bright_white]But first: cycle {cycle}.[/bright_white]",
        "I'm Idaho! [bright_white](Cycle {cycle}.)[/bright_white]",
    ],
    "scanning": [
        "Scanning [bright_white]{count}[/bright_white] repos for pull requests. [bright_white]I can see them with my eyes![/bright_white]",
        "Looking at the pull requests. [bright_white]The doctor said I should look with my eyes and not my hands.[/bright_white] I'll try.",
        "My daddy's police radio found [bright_white]{count}[/bright_white] repos. [bright_white]I'm scanning them now.[/bright_white]",
        "The pointy kitty took it! [bright_white]Wait, no. Scanning {count} repos.[/bright_white]",
    ],
    "scan_done": [
        "Found [bright_white]{total}[/bright_white] open pull requests across [bright_white]{repos}[/bright_white] repos! [bright_white]I found them![/bright_white]",
        "[bright_white]{total}[/bright_white] pull requests! I beat the smart PRs! [bright_white]I beat the smart PRs![/bright_white]",
        "I see [bright_white]{total}[/bright_white] pull requests. [bright_white]Some of them look sleepy.[/bright_white]",
    ],
    "merging": [
        "Merging PR [bright_white]#{pr}[/bright_white] in [bright_white]{repo}[/bright_white]. [bright_white]Squash! Like the game![/bright_white]",
        "PR [bright_white]#{pr}[/bright_white] is merge ready! [bright_white]I'm pressing the merge button! It's a big button![/bright_white]",
        "My daddy merges things. [bright_white]Now I'm merging PR #{pr} in {repo}![/bright_white]",
    ],
    "merged": [
        "PR [bright_white]#{pr}[/bright_white] merged! [bright_white]Everybody's hugging![/bright_white]",
        "Merged! [bright_white]I did it! I merged the PR! No wait, that IS me![/bright_white]",
        "PR [bright_white]#{pr}[/bright_white] in [bright_white]{repo}[/bright_white]: squash-merged and branch deleted. [bright_white]I fell out two times but the third time I got it.[/bright_white]",
    ],
    "merge_failed": [
        "I couldn't merge PR [bright_white]#{pr}[/bright_white]. [bright_white]I'm pedaling backwards![/bright_white]",
        "PR [bright_white]#{pr}[/bright_white] merge failed. [bright_white]I glued my head to my shoulder.[/bright_white]",
        "Merge failed for [bright_white]#{pr}[/bright_white]. [bright_white]Wheee!... ow, I bit my tongue.[/bright_white]",
    ],
    "reviewing": [
        "Reading PR [bright_white]#{pr}[/bright_white] in [bright_white]{repo}[/bright_white]. [bright_white]I'm learnding what they changed![/bright_white]",
        "Reviewing PR [bright_white]#{pr}[/bright_white]. [bright_white]The doctor said I should use my words. I'll use review words.[/bright_white]",
        "Looking at PR [bright_white]#{pr}[/bright_white] very carefully. [bright_white]With my eyes.[/bright_white]",
    ],
    "reviewed_approved": [
        "PR [bright_white]#{pr}[/bright_white] in [bright_white]{repo}[/bright_white]: [green]APPROVED![/green] [bright_white]It's a good one! Like a moon rock but for code![/bright_white]",
        "Approved PR [bright_white]#{pr}[/bright_white]! [bright_white]I beat the smart code![/bright_white]",
        "PR [bright_white]#{pr}[/bright_white]: [green]approved[/green]. [bright_white]Then I said 'This looks right' and the baby looked at me.[/bright_white]",
    ],
    "reviewed_changes": [
        "PR [bright_white]#{pr}[/bright_white] in [bright_white]{repo}[/bright_white] needs [yellow]{count}[/yellow] fixes. [bright_white]It tastes like... burning.[/bright_white]",
        "Left [yellow]{count}[/yellow] review comments on PR [bright_white]#{pr}[/bright_white]. [bright_white]I used my words![/bright_white]",
        "PR [bright_white]#{pr}[/bright_white]: [yellow]{count} findings[/yellow]. [bright_white]The pointy kitty made those problems.[/bright_white]",
    ],
    "discovering": [
        "Looking for work in [bright_white]{count}[/bright_white] repos. [bright_white]My daddy says always be busy or you get in trouble.[/bright_white]",
        "Discovering work items! [bright_white]I'm like a detective! A code detective![/bright_white]",
        "Checking the repos for things to do. [bright_white]This is my sandbox. I'm not allowed to go in the deep end.[/bright_white]",
    ],
    "discovered": [
        "Found [bright_white]{added}[/bright_white] new work items! Queue is now [bright_white]{total}[/bright_white] deep. [bright_white]I found them in my nose![/bright_white]",
        "[bright_white]{added}[/bright_white] new items discovered. [bright_white]I'm learnding there is always more work.[/bright_white]",
        "Work queue: [bright_white]{total}[/bright_white] items. [bright_white]When I grow up I'm going to do all of them![/bright_white]",
    ],
    "executing": [
        "Sending [bright_white]{count}[/bright_white] agents out to do work! [bright_white]Bye agents! Thanks for not eating me![/bright_white]",
        "Spawning [bright_white]{count}[/bright_white] Claude agents! [bright_white]I kissed a light socket once and I woke up in a helicopter. This is like that but on purpose.[/bright_white]",
        "[bright_white]{count}[/bright_white] agents going! [bright_white]I'm not allowed to go in the deep end but THEY are![/bright_white]",
    ],
    "agent_done": [
        "Agent done! [bright_white]{success}/{total} succeeded.[/bright_white] [bright_white]I'm wearing a bathrobe and I'm not even sick![/bright_white]",
        "Agents finished! [bright_white]{success}[/bright_white] good, [bright_white]{failed}[/bright_white] sad. [bright_white]And when the doctor told me I didn't have worms anymore, that was the happiest day of my life.[/bright_white]",
        "[bright_white]{success}/{total}[/bright_white] agents succeeded. [bright_white]I fell out two times but most of them didn't![/bright_white]",
    ],
    "pr_created": [
        "New PR created: [bright_white]{url}[/bright_white] [bright_white]I made a baby! I mean, I made a pull request![/bright_white]",
        "PR at [bright_white]{url}[/bright_white]! [bright_white]Mrs. Krabappel and Principal Skinner were in the closet making PRs and I saw one of the PRs and the PR looked at me.[/bright_white]",
        "Pull request opened: [bright_white]{url}[/bright_white] [bright_white]I did it! I have the best PR in the class![/bright_white]",
    ],
    "agent_failed": [
        "Agent failed on [bright_white]{task}[/bright_white]. [bright_white]I wet my arm pants.[/bright_white]",
        "[bright_white]{task}[/bright_white] didn't work. [bright_white]I ate too much plastic candy.[/bright_white]",
        "Agent for [bright_white]{task}[/bright_white] had an owie. [bright_white]I can't breathe good and it's making me sleepy.[/bright_white]",
    ],
    "sleeping": [
        "Sleeping for [bright_white]{seconds}[/bright_white] seconds. [bright_white]Oh boy, sleep! That's where I'm a Viking![/bright_white]",
        "Waiting [bright_white]{seconds}[/bright_white]s. [bright_white]This is my sandbox. I'm waiting in the not-deep-end.[/bright_white]",
        "Taking a [bright_white]{seconds}[/bright_white]-second nap. [bright_white]I think I'll have a good dream about code.[/bright_white]",
    ],
    "shutdown": [
        "Shutting down after [bright_white]{cycles}[/bright_white] cycles. [bright_white]Bye! Thanks for not eating me![/bright_white]",
        "[bright_white]{cycles}[/bright_white] cycles complete. [bright_white]I'm done! I dress myself and now I'm also done![/bright_white]",
        "Goodbye! [bright_white]And when the doctor told me I was done, that was the happiest day of my life.[/bright_white]",
    ],
    "error": [
        "[red]Something went wrong.[/red] [bright_white]It tastes like... burning.[/bright_white]",
        "[red]Error detected.[/red] [bright_white]That's where I saw the leprechaun. He tells me to burn things.[/bright_white]",
        "[red]Uh oh.[/red] [bright_white]Miss Hoover, there's a dog in the vent. Also there's an error.[/bright_white]",
        "[red]Something broke![/red] [bright_white]Principal Skinner, I got carsick in your office.[/bright_white]",
    ],
    "warning": [
        "[yellow]Heads up.[/yellow] [bright_white]Daddy, I'm scared. Too scared to even wet my pants.[/bright_white]",
        "[yellow]Warning.[/yellow] [bright_white]If mommy's purse didn't belong in the microwave, why did it fit?[/bright_white]",
        "[yellow]Something seems off.[/yellow] [bright_white]My face is on fire! Well, not MY face. But something's on fire.[/bright_white]",
        "[yellow]Caution.[/yellow] [bright_white]I can't breathe good and it's making me sleepy. Pay attention.[/bright_white]",
    ],
    "budget_warning": [
        "[red]BIG BUDGET WARNING.[/red] [bright_white]That's where I saw the leprechaun. He tells me to burn things. And budget.[/bright_white]",
        "[red]EXPENSIVE MODE ACTIVE.[/red] [bright_white]I kissed a light socket and woke up in a helicopter. This is like that for your API bill.[/bright_white]",
        "[red]HIGH COST AHEAD.[/red] [bright_white]If mommy's API key didn't belong in the expensive mode, why did it fit?[/bright_white]",
    ],
    "no_work": [
        "No work items found! [bright_white]I'm wearing a bathrobe and I'm not even sick! Everything is done![/bright_white]",
        "Queue is empty. [bright_white]Everybody's hugging![/bright_white]",
        "Nothing to do. [bright_white]Oh boy, sleep! That's where I'm a Viking! And there's no work to do![/bright_white]",
    ],
    "recovery": [
        "[yellow]Recovering from an error.[/yellow] [bright_white]I cheated wrong. I'll try again.[/bright_white]",
        "[yellow]Retrying after failure.[/yellow] [bright_white]I fell out two times. This is the third time.[/bright_white]",
        "[yellow]Coming back.[/yellow] [bright_white]I kissed a light socket once and I woke up in a helicopter. I'm back now.[/bright_white]",
    ],
    "orphan_recovery": [
        "Found [bright_white]{count}[/bright_white] orphaned tasks from before. [bright_white]They got lost like when I got lost at the mall except they're tasks.[/bright_white]",
        "Recovering [bright_white]{count}[/bright_white] old active runs. [bright_white]My daddy the policeman always goes back for the lost ones.[/bright_white]",
    ],
    "force_warning": [
        "[red]⚠ FORCE OPERATIONS ACTIVE.[/red] [bright_white]Ordinary pull requests destroyed the repo, Rick. They should be ruled with an iron hand.[/bright_white]",
        "[red]⚠ MERCY DISABLED.[/red] [bright_white]I'm not doing this for fun. Well... I'm a little bit doing this for fun.[/bright_white]",
    ],
}

# ── Variant-specific personality overrides ──────────────────────────────────
# Some variants get special messages that replace the generic ones

_VARIANT_OVERRIDES: dict[Variant, dict[str, list[str]]] = {
    Variant.OLD_MAN: {
        "startup": [
            "[dark_red]Ordinary developers destroyed the codebase, Rick.[/dark_red] [bright_white]Not the PM. Not the stakeholders. Ordinary damned developers. I'm here now.[/bright_white]",
            "[dark_red]The Maestro has arrived.[/dark_red] [bright_white]I've been alive for a hundred years and I still have to deal with merge conflicts.[/bright_white]",
            "[dark_red]I have seen every PR ever opened. I have won every argument.[/dark_red] [bright_white]Let's begin.[/bright_white]",
        ],
        "merging": [
            "[dark_red]Imposing vision on PR [bright_white]#{pr}[/bright_white].[/dark_red] [bright_white]The trophy room grows.[/bright_white]",
            "[dark_red]PR [bright_white]#{pr}[/bright_white] in [bright_white]{repo}[/bright_white]: force-merging.[/dark_red] [bright_white]-X ours. My branch is correct. It has always been correct.[/bright_white]",
        ],
        "shutdown": [
            "[dark_red]Done.[/dark_red] [bright_white]I have ruled. I have executed. I have prevailed. Goodbye, Rick.[/bright_white]",
            "[dark_red]The work is complete.[/dark_red] [bright_white]They should have listened the first time.[/bright_white]",
        ],
        "force_warning": [
            "[bright_red]⚠ MAESTRO MODE.[/bright_red] [dark_red]Your branch wins. Always.[/dark_red] [bright_white]They should be ruled with an iron hand.[/bright_white]",
        ],
    },
    Variant.SAVAGE: {
        "startup": [
            "[bright_green]RALPH START.[/bright_green] [bright_white]RALPH DO WORK. RALPH NOT STOP.[/bright_white]",
            "[bright_green]OH BOY.[/bright_green] [bright_white]OH BOY OH BOY OH BOY. WORK TIME. RALPH LOVE WORK TIME.[/bright_white]",
        ],
        "sleeping": [
            "[bright_green]RALPH NOT SLEEP.[/bright_green] [bright_white]RALPH GO AGAIN. IMMEDIATELY.[/bright_white]",
        ],
        "budget_warning": [
            "[bright_red]RALPH NOT CARE ABOUT TOKENS.[/bright_red] [bright_white]RALPH CARE ABOUT WORK. RALPH DO WORK NOW.[/bright_white]",
        ],
    },
    Variant.IMMORTAL: {
        "recovery": [
            "[dark_green]I came back again.[/dark_green] [bright_white]I always come back. The error didn't know that. The error knows now.[/bright_white]",
            "[dark_green]Error cleared.[/dark_green] [bright_white]You can't keep me down. Ask anyone.[/bright_white]",
            "[dark_green]Recovered.[/dark_green] [bright_white]That's where I saw the leprechaun. He tells me to burn things. I came back anyway.[/bright_white]",
        ],
        "sleeping": [
            "[dark_green]Sleeping [bright_white]{seconds}[/bright_white]s before trying again.[/dark_green] [bright_white]I'll be back. I'm always back. Oh boy, sleep! That's where I'm a Viking who never dies![/bright_white]",
        ],
    },
    Variant.BLUE: {
        "startup": [
            "[bright_blue]Observation mode active.[/bright_blue] [bright_white]I'm looking with my eyes and not my hands. I will continue to look with my eyes and not my hands.[/bright_white]",
            "[bright_blue]Read-only mode.[/bright_blue] [bright_white]The doctor said I wouldn't have so many nosebleeds if I kept my finger out of there. I'm keeping my finger out of the code.[/bright_white]",
        ],
        "reviewing": [
            "[bright_blue]Reading PR [bright_white]#{pr}[/bright_white] very carefully.[/bright_blue] [bright_white]With my eyes. Not my hands.[/bright_white]",
        ],
    },
    Variant.PROFESSOR: {
        "startup": [
            "[magenta]Professor Ralph online.[/magenta] [bright_white]I went to therapy and now I'm all one person. Let's think before we act.[/bright_white]",
            "[magenta]Integrated mode.[/magenta] [bright_white]Me fail English? That's unpossible. Me fail planning phase? Also unpossible. Let's begin.[/bright_white]",
        ],
        "discovering": [
            "[magenta]Planning phase:[/magenta] [bright_white]reading ARCHITECTURE.md, DESIGN.md, STATE.md, git log, open PRs... I'm learnding the whole picture first.[/bright_white]",
        ],
    },
    Variant.WORLD_BREAKER: {
        "startup": [
            "[bright_red]WORLD BREAKER RALPH ONLINE.[/bright_red] [bright_white]I don't want to hurt you. I don't want to help you. But if you get in my way — every agent at opus. Every repo. No sleep.[/bright_white]",
            "[bright_red]CONFIRMED: --confirm-burn-everything.[/bright_red] [bright_white]Oh boy. Oh boy oh boy. I kissed a light socket once and woke up in a helicopter. This is like that for your entire API bill.[/bright_white]",
        ],
        "budget_warning": [
            "[bright_red]⚠ EVERY AGENT IS OPUS.[/bright_red] [bright_white]I don't want to hurt your bill. I don't want to help your bill. If your bill gets in the way—[/bright_white]",
        ],
    },
    Variant.JOE_FIXIT: {
        "startup": [
            "[grey62]Joe Fixit Ralph. Single repo. {cycles} cycles. Then I'm gone.[/grey62] [bright_white]Look, I'm not doing this for the good of humanity. I dress myself.[/bright_white]",
            "[grey62]Fine. I'll do it.[/grey62] [bright_white]I'm not the strongest one there is, but I'm smart about where I apply it.[/bright_white]",
        ],
        "shutdown": [
            "[grey62]Done. {cycles} cycles. Here's your bill.[/grey62] [bright_white]I fell out two times but I got the job done. Pay me in PRs.[/bright_white]",
        ],
    },
}


def _get_quotes(variant: Variant, category: str) -> list[str]:
    """Get quote list for variant+category, falling back to generic."""
    overrides = _VARIANT_OVERRIDES.get(variant, {})
    if category in overrides:
        return overrides[category]
    return _QUOTES.get(category, [f"Ralph is doing [bright_white]{category}[/bright_white] things."])


def ralph_says(
    variant: Variant,
    category: str,
    **kwargs: str | int,
) -> None:
    """Print a Rich-formatted Ralph message for the given variant and event.

    Selects a random quote from the appropriate category, formats it with
    kwargs (e.g. cycle=3, repo="kings-road"), and prints with variant color.

    Args:
        variant: Which Ralph persona is speaking.
        category: Event category (startup, merging, error, etc.).
        **kwargs: Template variables to substitute into the quote text.
    """
    quotes = _get_quotes(variant, category)
    quote = random.choice(quotes)

    # Format kwargs into the quote
    try:
        formatted = quote.format(**kwargs)
    except KeyError:
        formatted = quote  # if formatting fails, Ralph speaks unformatted

    colors = _COLORS[variant]
    name_color = colors["primary"]

    rendered = f"[{name_color}]\\[{variant.value}][/{name_color}] {formatted}"
    _RECENT_EVENTS.append((variant, rendered, datetime.now(UTC)))

    console.print(rendered, highlight=False)


def ralph_panel(
    variant: Variant,
    category: str,
    title: str | None = None,
    **kwargs: str | int,
) -> None:
    """Print a Rich Panel with a Ralph message. Used for startup/shutdown banners.

    Args:
        variant: Which Ralph persona is speaking.
        category: Event category.
        title: Panel title override. Defaults to the variant name.
        **kwargs: Template variables.
    """
    quotes = _get_quotes(variant, category)
    quote = random.choice(quotes)

    try:
        formatted = quote.format(**kwargs)
    except KeyError:
        formatted = quote

    colors = _COLORS[variant]
    panel_title = title or f"[{colors['primary']}]{variant.value}[/{colors['primary']}]"

    text = Text.from_markup(formatted)
    console.print(Panel(text, title=panel_title, border_style=colors["primary"]))
