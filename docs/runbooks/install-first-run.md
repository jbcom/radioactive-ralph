---
title: Install + first run
description: Install the radioactive_ralph binary, initialize a repo, and do a first runner-backed run.
---

This is the canonical first-time flow. Use the package-manager path
that matches your platform, then drop into the common post-install
section.

## 1a. Homebrew (macOS, Linux, WSL2 + Linuxbrew)

```sh
# Explicit URL form — the repo is named `pkgs`, not `homebrew-pkgs`,
# so the convention-shortcut `brew tap jbcom/pkgs` alone doesn't
# resolve. Pass the URL and brew taps it correctly.
brew tap jbcom/pkgs https://github.com/jbcom/pkgs
brew install radioactive-ralph
radioactive_ralph --version
```

Expected: the version string matches whatever tag you're installing
(e.g. `0.8.1 (99536d0, built 2026-04-16T...)`).

## 1b. Scoop (Windows)

```powershell
scoop bucket add jbcom https://github.com/jbcom/pkgs
scoop install radioactive-ralph
radioactive_ralph --version
```

## 1c. curl installer (macOS, Linux, WSL2)

```sh
curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh
radioactive_ralph --version
```

The installer writes to `/usr/local/bin` if writable, else
`~/.local/bin`. On the latter, make sure `~/.local/bin` is on `$PATH`.

## 2. Initialize the repo

`cd` into any git repo and run:

```sh
radioactive_ralph init
```

This scaffolds `.radioactive-ralph/` with:

- `config.toml` — operator choices (committed)
- `local.toml` — machine-local overrides (gitignored)
- `plans/index.md` — human-readable plan index

Re-runnable. Pass `--yes` for non-interactive (CI) mode, or `--force`
to overwrite an existing `config.toml`.

## 3. Verify your environment

```sh
radioactive_ralph doctor
```

Expected OK lines: `git`, your provider CLI (`claude`, `codex`, or
`gemini`), optional service-manager hook (launchd on macOS, systemd on
Linux, SCM on Windows). See
[Provider auth](./provider-auth.md) if a provider check fails.

## 4. Start the repo-scoped runtime

```sh
radioactive_ralph service start
```

Runs the durable repo service in the foreground. On launchd/systemd,
you'll more commonly `service install` (see [Service runbook](./service.md))
which registers the runtime to start at login. For a first run, keep
it in the foreground so you can see the control-plane logs.

In another terminal:

```sh
radioactive_ralph status --json
```

Expected: a JSON body with `repo_path`, `pid`, `started_at`, and an
empty `workers` array. If you see `no service socket at <path>`, the
service isn't running yet — go back to step 4.

## 5. Create your first plan

```sh
radioactive_ralph run --variant fixit --advise --topic bootstrap
```

Fixit runs its six-stage plan-creation pipeline against the repo and:

1. Writes `.radioactive-ralph/plans/bootstrap-advisor.md` (the
   human-readable plan)
2. Emits the same plan into the plan DAG (durable SQLite under your
   XDG state dir)
3. Prints the next-step command

See the [fixit delegation guide](../guides/fixit-delegation.md) for
the full pipeline.

## 6. Supervise a plan

With a plan in place, any non-fixit variant can claim tasks:

```sh
radioactive_ralph run --variant grey
```

The runner polls the DAG for ready tasks, dispatches each to the
provider subprocess (your configured Claude / Codex / Gemini), and
marks tasks done/failed based on acceptance criteria.

## 7. Open the cockpit

```sh
radioactive_ralph tui
```

Shows active plans, workers, recent events. See the [TUI guide](../guides/tui.md)
for keyboard shortcuts and drilldowns.

## When something goes wrong

See [Troubleshooting](./troubleshooting.md) for the common failure
modes (stale heartbeat, dead socket, provider CLI missing).
