---
title: Design
description: The product shape and why it's built this way.
---

# Design

## The shape

radioactive-ralph is one binary with one job: supervise AI-agent CLIs so
they can run for hours without an operator watching them, without ever
blocking on a prompt they can't answer.

- **One binary**, two modes: `--supervisor` (the control layer) and the
  plain dumb client (a read-only view).
- **One user-level database** — durable memory for every project on the
  machine, never a file committed to a repo.
- **One mutating Ralph.** There is no lineup of personas; the same agent
  becomes whatever a task needs, driven by the plan and its context.
- **Providers as bindings**, not the identity of the product. Claude,
  Codex, and OpenCode are interchangeable execution backends behind the
  same contract.

## Why personas were removed

An earlier iteration of this project modeled Ralph as a family of
personas (blue, green, savage, fixit, and others), each with a baked
system prompt and its own safety-floor configuration. That model added a
whole surface — persona registries, per-persona confirmation gates,
per-persona provider bindings — for a problem better solved by the plan
itself: what runs, how much parallelism, and what's safe to do are all
properties of the task at hand, not of which character is "on shift."
Removing personas simplified every prompt to *"you are an agent; here is
your task; here is the necessary context"* and collapsed a large
surface of duplicated safety-floor logic into the orchestrator's single
verification path.

## Why one supervisor, not a multiplexer

Early designs considered running agents inside tmux panes for
observability. tmux was rejected: the tmux server owns the pty, so every
read/write/kill becomes an `os/exec` round-trip to a process the
supervisor doesn't control — which breaks the never-block invariant (the
supervisor can't reliably watch or kill what it doesn't own) and adds an
external binary as a failure domain. `creack/pty` gives the supervisor
direct ownership of every agent's stdio instead.

## Why plans are markdown, not a DSL

Plans are parsed with `goldmark` and decomposed heuristically: heading
nesting encodes grouping and ordering, list type (ordered vs. unordered)
encodes sequential vs. parallel steps. No LLM is involved in
decomposition — the grammar is simple enough that a plan document reads
naturally and parses deterministically. See
[Plan format](./plan-format.md) for the grammar and
[Provider contract](../design/provider-contract.md) for how steps are
dispatched to agents.

## Provider direction

Each provider binding is a capability record — how to invoke it
non-interactively, how to read its structured result and usage/cost, how
it resumes, and whether it natively fans out subagents. `claude`,
`codex`, and `opencode` ship today. `gemini` was removed after its CLI's
auth endpoint was deprecated (2026-06-18); `cursor-agent` is excluded
because it delegates session control to Cursor's cloud rather than
running locally. Adding a provider is a table-driven registration, not a
rearchitecture.
