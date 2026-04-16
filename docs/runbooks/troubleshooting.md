---
title: Troubleshooting
description: Failure modes you'll hit in practice and the fix for each.
---

This is the triage guide. Match your symptom, apply the fix, then
re-check `radioactive_ralph doctor` to confirm.

## Stale heartbeat

### Symptom

`radioactive_ralph status` returns a response but with stale data, or
says the service is running but `run --variant ...` hangs waiting for
the control plane.

### What it means

The service process crashed without cleaning its control-plane socket
and heartbeat file. The socket / named pipe still exists on disk, so
clients connect successfully — but nothing is on the other end.

### Fix

```sh
radioactive_ralph service clean
radioactive_ralph service start   # or launchctl/systemctl/SCM restart
```

Verify:

```sh
radioactive_ralph status --json
```

Response should have a fresh `started_at` timestamp.

## Dead socket or pipe

### Symptom (Unix)

```
error: ipc: dial /path/to/control.sock: connection refused
```

### Symptom (Windows)

```
error: ipc: dial \\.\pipe\radioactive-ralph-<slug>: pipe does not exist
```

### Fix

Either the service never started or the OS cleaned the endpoint.

```sh
# 1. Is the service actually running?
pgrep -f "radioactive_ralph service"        # Unix
Get-Process | Where-Object { $_.ProcessName -like "*radioactive*" }   # Windows

# 2a. If yes: the endpoint is stale — clean + restart
radioactive_ralph service clean
radioactive_ralph service start

# 2b. If no: just start it
radioactive_ralph service start
```

On Windows, named pipes are killed by reboot even with an installed
service. That's normal — the service wrapper recreates the pipe on
start. Don't panic.

## Provider CLI missing or unauthenticated

### Symptom

```
doctor: [FAIL] claude — claude binary not on PATH
```

or

```
service: worker failed to spawn: claude -p: exit 1 (sign in required)
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

If you're not using claude/codex/gemini, remove the variant's
`provider = ...` line from `config.toml` instead of installing a
provider you won't use.

## Service-install errors

### Symptom (macOS)

```
launchctl bootstrap failed: 5: Input/output error
```

`5: I/O error` from launchctl usually means the plist is
syntactically-valid but references a binary launchctl can't execute
(permissions or SIP).

### Fix (macOS)

```sh
# 1. Verify the plist exists and the binary path is correct
cat ~/Library/LaunchAgents/com.jbcom.radioactive-ralph.<slug>.plist

# 2. Verify the binary is executable
ls -l $(which radioactive_ralph)

# 3. Re-install
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

## Worker stuck in `running`

### Symptom

`radioactive_ralph plan tasks <plan> --status running` shows a task
claimed a long time ago with no progress, no events in
`plan history`, and the variant subprocess is gone.

### What it means

The runtime's reaper hasn't swept it yet. The task has a claimed-by
session that's dead; the reaper releases claims on sessions whose
heartbeat is stale.

### Fix

```sh
# Force-requeue the stuck task (resets claim, back to ready)
radioactive_ralph plan requeue <plan> <task>
```

If the same task keeps getting stuck:

```sh
# Mark it failed and look at the run history
radioactive_ralph plan fail <plan> <task>
radioactive_ralph plan history <plan> <task>
```

## plandag schema mismatch

### Symptom

```
error: plandag: open: schema version 5 is newer than this binary's supported 4
```

### What it means

You downgraded the binary. The plandag SQLite file has a newer
user_version than the running binary knows how to read.

### Fix

- Re-install the newer binary (recommended)
- Or nuke the state and re-init (if you don't care about plan
  history):
  ```sh
  rm -rf $XDG_STATE_HOME/radioactive-ralph/<repo-hash>/plans.db
  radioactive_ralph init
  ```

## Fixit advisor writes a fallback plan

### Symptom

```
ralph: fixit emitted a fallback plan — operator intervention required
```

### What it means

Stage 4 (Claude analysis) or Stage 5 (validation) failed twice. Fixit
wrote a diagnostic plan (`status: fallback`) instead of guessing.

### Fix

Open `.radioactive-ralph/plans/<topic>-advisor.md` and read the
"Methodology" section — it includes the raw Claude output and the
validation errors. Typical causes:

- Claude returned non-JSON (rare with the opus tier; usually a
  timeout)
- Operator constraints conflict (e.g. `--description` forbids all
  variants)
- Repo state makes every variant disqualified (e.g. operator is on a
  release branch)

Adjust the inputs and re-run `run --variant fixit --advise`.

## When everything else fails

Capture state and open an issue:

```sh
radioactive_ralph doctor --verbose > /tmp/ralph-doctor.txt
radioactive_ralph status --json   > /tmp/ralph-status.json
journalctl --user -u radioactive-ralph-<slug> --since -1h > /tmp/ralph-journal.txt
```

(Or the platform equivalent — macOS Console, Windows Event Viewer.)

File at <https://github.com/jbcom/radioactive-ralph/issues> with those
three files attached.
