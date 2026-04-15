---
title: Plan format (PRQ / PRD)
description: What a plan looks like on disk, how fixit imports it into the plan DAG, and how supervised variants execute it.
---

Radioactive Ralph is **plans-first**: every variant except `fixit`
refuses to boot unless there's at least one active plan in the plan
DAG. This page describes the plan format that `fixit --advise`
produces and that `radioactive_ralph run --advise --plan <file>`
accepts.

## On-disk layout

Plans live in `.radioactive-ralph/plans/` (committed to the repo) as
markdown files with YAML frontmatter:

```text
.radioactive-ralph/
├── config.toml         # operator choices (committed)
├── local.toml          # machine-local overrides (gitignored)
└── plans/
    ├── index.md        # list of active plan files
    └── <slug>.prq.md   # one file per plan
```

`index.md` is a human-readable index. The source of truth for task
state is the plan DAG SQLite database, not the markdown — plans are
imported into the DAG via `radioactive_ralph plan import`.

## Frontmatter

```yaml
---
title: v0.2 Stack Redesign — Full Rewrite
updated: 2026-04-14
status: current       # current | draft | stale | archived
priority: HIGH
timeframe: 1 week
slug: v0-2-stack-redesign
primary_variant: green
confidence: 85
---
```

Only `title` and `slug` are required. `status: current` means the plan
is eligible for execution; `status: draft` means fixit is still
refining it; anything else is skipped by `run --advise`.

## Task list

Tasks are markdown checklist items, optionally grouped under `###`
headings (epics):

```markdown
### Epic A — Tear Down

- [ ] P1: Delete `src/sim/turf/slot-accessors.ts`
- [ ] P1: Delete `src/sim/turf/pack-resolver.ts`

### Epic B — Build Up

- [ ] P2: Add stack-based turf model in `src/sim/stack/`
  depends: delete-slot-accessors delete-pack-resolver
  variant: green
  effort: medium
```

fixit's importer recognizes:

- **Check state** — `- [ ]` pending, `- [x]` done
- **Priority prefix** — `P1:`, `P2:`, `P3:` become task priorities
- **Inline metadata** (indented lines under a task):
  - `depends: <slug1> <slug2> ...` — task dependencies
  - `variant: <name>` — preferred variant (green, professor, fixit, …)
  - `effort: low | medium | high | auto`
  - `context: boundary` — marks this task as a context-reset boundary
  - `acceptance:` — JSON-like acceptance criteria payload

## Import flow

```sh
radioactive_ralph plan import .radioactive-ralph/plans/v0-2.prq.md
```

runs the markdown parser in `internal/fixit` (via goldmark, not
regex), produces a DAG of `CreateTaskOpts` + `AddDep` calls, and
writes them into the `plandag` SQLite store. Tasks inherit the plan's
`primary_variant` unless overridden; dependencies default to the
previous task in document order.

## Execution

Once imported, any variant can claim ready tasks:

```sh
radioactive_ralph run --variant green --detach
```

The supervisor polls `plandag.ClaimNextReady` in a loop, hands each
claimed task to the managed Claude subprocess as a stream-json user
message, and marks the task done/failed based on the subprocess's
response. See [fixit delegation](/guides/fixit-delegation/) for the
full plan-creation pipeline and [supervisor loop](/reference/architecture/)
for the execution loop.
