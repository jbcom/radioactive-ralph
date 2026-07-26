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

## Provider selection and Ralph-managed pools

A project may select one provider:

```toml
provider = "codex"
```

or a round-robin pool:

```toml
providers = ["claude", "codex", "opencode"]
```

The plural form is not a persona list. It is an execution policy: each
dispatched plan step receives its own Ralph worker and successive workers are
distributed across the named provider capability records. Ralph therefore
suppresses `NativeFanout` for pooled bindings; an unordered group remains a
set of visible, independently supervised tasks instead of collapsing into one
opaque provider invocation. The plural key wins when both forms are present.
Empty, non-string, or duplicate pool entries fail loudly.

Process-wide concurrency is a supervisor resource limit, not project
identity. Set it on the environment of the supervisor process, using the
platform-specific form below.

On macOS and Linux, install or update the user service with the environment
attached:

```sh
# Replace N with an operator-chosen positive-integer emergency ceiling.
radioactive_ralph service install --env RALPH_MAX_PARALLEL=N
```

On native Windows, SCM install/start is disabled. The supported native process
is a control plane running in a foreground PowerShell terminal:

```powershell
$env:RALPH_MAX_PARALLEL = "N"
radioactive_ralph --supervisor
```

Keep that terminal open and run `radioactive_ralph` as the client from another
terminal. Native Windows provider PTYs are unsupported, so use WSL2 for
provider-backed execution. Inside WSL2, use the Linux service command above;
the WSL2 `systemd --user` service is the functional unattended route.

When configured, `RALPH_MAX_PARALLEL` must be a non-empty integer from `1`
through `256`. Invalid or explicitly blank values are rejected before the
installed service is rewritten or restarted.
That range is validation, not an operating recommendation. When unset, v0.22
preserves the historical unbounded behavior for compatibility; unbounded is
neither adaptive nor recommended as an optimum.

## Project identity

Config is keyed by project ID, not by path. See
[Architecture](../reference/architecture.md#project-identity) for how a
project is identified by accumulated fingerprints rather than an absolute
path.
