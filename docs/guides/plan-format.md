---
title: Plan format (PRQ / PRD)
description: How repo-visible plan files relate to the live SQLite plan DAG, and what `plan import` accepts today.
---

Radioactive Ralph is **plans-first**: every variant except `fixit`
expects an active plan in the SQLite plan store. The important nuance is
that the repo-visible markdown files are documentation and operator
artifacts, while the executable source of truth lives in the state-dir
database.

## Two layers of plan state

### Repo-visible files

```text
.radioactive-ralph/
├── config.toml
├── local.toml
└── plans/
    ├── index.md
    └── <topic>-advisor.md
```

These files are committed and reviewable. They explain intent, operator
context, and fixit recommendations, but they are not the execution
engine.

### Live DAG store

The runnable plan state lives under the Ralph state root in SQLite
(`plans.db`). That store tracks plan IDs, tasks, dependencies, claims,
retries, and lifecycle events.

## What `init` creates

`radioactive_ralph init` seeds the repo with human-facing scaffolding so
operators have an explicit place to point Ralph:

- `.radioactive-ralph/plans/index.md` as a human-facing landing page
- per-repo config and local override files

## What fixit writes

`radioactive_ralph run --variant fixit --advise` writes a markdown report to:

```text
.radioactive-ralph/plans/<topic>-advisor.md
```

That report is for humans. It records the recommendation, tradeoffs,
and suggested tasks.

On first creation for a given repo/topic slug, fixit also syncs the
proposal into the durable SQLite plan DAG. If a plan with the same slug
already exists for that repo, the markdown report is refreshed and the
existing DAG plan is left untouched.

## What `plan import` accepts today

Today, `radioactive_ralph plan import` accepts a **JSON file**, not a PRQ
markdown document:

```bash
radioactive_ralph plan import ./plan-bootstrap.json
```

The JSON importer creates a new plan and tasks in the SQLite store.
That is the current supported machine-ingest path.

## Practical workflow

1. Run `radioactive_ralph init`.
2. Ask fixit for advice with `radioactive_ralph run --variant fixit --advise ...`.
3. Review `.radioactive-ralph/plans/<topic>-advisor.md`.
4. Inspect the durable plan with `radioactive_ralph plan ls` and `radioactive_ralph plan show <slug>`.
5. If you need to seed executable tasks programmatically, import JSON with `radioactive_ralph plan import`.
6. Execute or inspect plans with `plan next` and `plan mark-done`.

## Current limitation

Markdown PRQ import is not the live path right now. If you want a
machine-loaded plan today, use fixit's advisor sync or feed JSON into
`plan import`; if you want human-readable planning, use the advisor
markdown reports.
