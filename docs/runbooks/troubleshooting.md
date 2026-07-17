---
title: Troubleshooting
description: Failure modes you'll hit in practice and the fix for each.
lastUpdated: 2026-07-16
---

This is the triage guide. Match your symptom, apply the fix, then
re-check `radioactive_ralph doctor` to confirm.

## Stale heartbeat

### Symptom

The client hangs, or reports the supervisor as up when it's actually
dead.

### What it means

The supervisor process crashed without cleaning its socket and heartbeat
file. The socket / named pipe still exists on disk, so a dial can
succeed at the OS level even though nothing answers — a fresh supervisor
startup detects this (dead PID behind the lockfile) and reclaims
automatically; a stuck client predates that reclaim.

### Fix

```sh
# 1. Stop the supervisor if it's somehow still alive.
pgrep -f "radioactive_ralph --supervisor" && kill <pid>

# 2. Remove the stale socket + heartbeat file by hand if needed.
rm -f "$HOME/Library/Application Support/radioactive-ralph/service.sock"
rm -f "$HOME/Library/Application Support/radioactive-ralph/service.sock.alive"

# 3. Restart the supervisor.
radioactive_ralph --supervisor   # or the OS service manager restart
```

Verify by running the client:

```sh
radioactive_ralph
```

## Dead socket or pipe

### Symptom (Unix)

```
error: ipc: dial /path/to/service.sock: connection refused
```

### Symptom (Windows)

```
error: ipc: dial \\.\pipe\radioactive_ralph-<token>-service: pipe does not exist
```

### Fix

Either the supervisor never started or the OS cleaned the endpoint.

```sh
# 1. Is the supervisor actually running?
pgrep -f "radioactive_ralph --supervisor"        # Unix
Get-Process | Where-Object { $_.ProcessName -like "*radioactive*" }   # Windows

# 2a. If yes: stop it, remove the stale endpoint, restart.
kill <pid>
rm -f "<state-root>/service.sock"   # Unix; Windows pipes clear on reboot
radioactive_ralph --supervisor

# 2b. If no: just start it.
radioactive_ralph --supervisor
```

On Windows, named pipes are killed by reboot even with an installed
service. That's normal — the service wrapper recreates the pipe on
start.

## Provider CLI missing or unauthenticated

### Symptom

```
doctor: [FAIL] claude — claude binary not on PATH
```

### Fix

See [Provider auth](./provider-auth.md) for the full flow. The short
version:

```sh
# Install if missing
curl -fsSL https://anthropic.com/install.sh | sh

# Authenticate
claude
# (first run prompts you to sign in at console.anthropic.com)

# Verify
radioactive_ralph doctor
```

If you're not using a given provider, ignore its doctor warning rather
than installing something you won't use.

## Service-install errors

### Symptom (macOS)

```
launchctl bootstrap failed: 5: Input/output error
```

`5: I/O error` from launchctl usually means the generated definition is
syntactically valid but references a binary launchctl can't execute
(permissions or SIP).

### Fix (macOS)

```sh
# 1. Verify the binary is executable
ls -l $(which radioactive_ralph)

# 2. Re-install
radioactive_ralph service uninstall
radioactive_ralph service install
```

### Symptom (Linux)

```
systemctl --user: Failed to connect to bus: No such file or directory
```

### Fix (Linux)

systemd `--user` requires a user session bus. Under SSH on a headless
box:

```sh
export XDG_RUNTIME_DIR=/run/user/$UID
loginctl enable-linger $USER     # keep the user bus alive across sessions
```

Then re-run `service install`.

### Symptom (Windows)

```
service install failed: access denied
```

### Fix (Windows)

Registering an SCM service requires admin privileges. Open PowerShell
**as administrator** and re-run `radioactive_ralph service install`.

## Worker stuck / no progress

### Symptom

A task looks claimed but no evidence and no state transition for a long
time, and the agent process is gone.

### What it means

The watchdog's stall/no-output detection should have killed and
reclaimed it already; if it hasn't, the reaper sweeps it on its next
pass. This is expected transient behavior, not a hang — the never-block
invariant means the system self-corrects rather than waiting.

### Fix

Give the reaper a pass, then re-check. If the same step keeps failing to
make progress across multiple reclaim cycles, that's a signal the step
itself (or its acceptance criteria) needs attention, not the runtime.

## When everything else fails

Capture state and open an issue:

```sh
radioactive_ralph doctor > /tmp/ralph-doctor.txt
radioactive_ralph --supervisor --log-format json 2> /tmp/ralph-supervisor.log &
```

(Or the platform log equivalent — `journalctl`, macOS Console, Windows
Event Viewer.)

File at <https://github.com/jbcom/radioactive-ralph/issues> with those
attached.
