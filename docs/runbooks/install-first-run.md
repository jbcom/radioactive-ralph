---
title: Install + first run
description: Install the radioactive_ralph binary, start the supervisor, and run the client.
lastUpdated: 2026-07-16
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

## 2. Verify your environment

```sh
radioactive_ralph doctor
```

Expected OK lines: `git`, a provider CLI (`claude`, `codex`, or
`opencode`), and an optional service-manager hook. See
[Provider auth](./provider-auth.md) if a provider check fails.

## 3. Start the supervisor

```sh
radioactive_ralph --supervisor
```

Runs the supervisor in the foreground. For daily use, install it as an
OS service instead (see [Service runbook](./service.md)):

```sh
radioactive_ralph service install
radioactive_ralph service status
```

For a first run, keep it in the foreground so you can see the logs
directly.

## 4. Register a project

In another terminal, from inside any project directory:

```sh
radioactive_ralph --init
```

This records the project's fingerprints (git root-commit + remote +
absolute path) and its config in the one user-level database. Nothing is
written into the project directory.

## 5. Run the client

```sh
radioactive_ralph
```

In a terminal, this discovers the running supervisor and renders the
read-only TUI. Piped or non-interactive, it prints a single status line:

```sh
radioactive_ralph 2>&1 | cat
```

If you see "no supervisor is running", go back to step 3.

## When something goes wrong

See [Troubleshooting](./troubleshooting.md) for common failure modes
(stale heartbeat, dead socket, provider CLI missing).
