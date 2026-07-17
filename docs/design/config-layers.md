---
title: Config virtual layers
description: How USER and PROJECTS config resolve through cobra/viper layers, backed by the one user-level database.
---

# Config virtual layers

Configuration is never a single committed file. It resolves through
virtual layers built by the supervisor from the one user-level database
plus three override flags (`internal/vconfig`).

## The three flags

- **`--config-file` / `-C`** — a joint config file; may contain a
  `projects:` stanza.
- **`--user-config-file`** — a user-specific config file; may also carry
  a `projects:` stanza.
- **`--project-config-file`** — config for one specific project; ignored
  in `--supervisor` mode.

## Two virtual layers, built in order

1. **Virtual USER config** (low → high precedence):
   `DB config` < `--config-file` < `--user-config-file`.
2. **Virtual PROJECTS config** (per project):
   `all projects from the DB` < the `projects:` stanza from the virtual
   USER config.

`viper` does the mechanical merge; `internal/vconfig` owns the DB layer,
the two-layer USER→PROJECTS composition, `projects:` stanza extraction,
and the change-vs-override distinction below.

## Changes vs. overrides

This distinction only matters for `--project-config-file`, and only
because the same flag means two different things depending on mode:

- **CHANGES** happen via `--init` (new or re-initialization). A passed
  `--project-config-file` here is merged onto the virtual `user.projects`
  config for that project **and persisted to the database**.
- **OVERRIDES** happen in normal client mode (no `--init`). The same flag
  here does not touch stored config — it merges on top of the virtual
  layer **at runtime only**.

## Conflict diffing

If project config arriving via `--config-file` or a `--user-config-file`
`projects:` stanza would override a stored project's settings, the
supervisor diffs stored vs. incoming values
(`vconfig.DiffConflicts`) and reports every colliding key. The resolution
is either to keep passing the conflicting keys as
`--project-config-file` (an explicit override, not a change) or to strip
them (`vconfig.AutoRemove`).

## Validation

Validation runs against the fully merged virtual layer
(`vconfig.Validate`), never against a single source. A missing required
key produces one error naming exactly what's missing, regardless of
whether the gap came from the DB, a file, or a flag.

## Project identity

Config is keyed by project ID, not by path. See
[Architecture](../reference/architecture.md#project-identity) for how a
project is identified by accumulated fingerprints rather than an absolute
path.
