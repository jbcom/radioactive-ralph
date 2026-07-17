---
title: Supervisor service install, start, stop, recover
description: Manage the supervisor as a per-user OS service on macOS, Linux, and Windows.
lastUpdated: 2026-07-16
---

`radioactive_ralph service ...` manages the supervisor
(`radioactive_ralph --supervisor`) as a per-user auto-restarting OS
service. The supervisor is the one long-lived process on the machine —
there is one per user, not one per repo.

## service install vs. running --supervisor directly

| Command | What it does | When to use |
|---------|--------------|-------------|
| `radioactive_ralph --supervisor` | Runs the supervisor **in the foreground** | First-run debugging, CI smoke tests, watching logs directly |
| `radioactive_ralph service install` | Registers the supervisor with your OS service manager (launchd / systemd / SCM) | Daily use; the supervisor auto-starts at login and auto-restarts on crash |

## 1. Install as an OS service

There is exactly one service definition per user per machine — not one
per repo — named `jbcom.radioactive-ralph.supervisor` (launchd) or
`radioactive_ralph-supervisor` (systemd / Windows SCM).

### macOS (launchd)

```sh
radioactive_ralph service install
```

Writes `~/Library/LaunchAgents/jbcom.radioactive-ralph.supervisor.plist`
and loads it. Verify:

```sh
launchctl list | grep radioactive-ralph
```

### Linux (systemd --user)

```sh
radioactive_ralph service install
```

Writes `~/.config/systemd/user/radioactive_ralph-supervisor.service` and
enables + starts it. Verify:

```sh
systemctl --user status radioactive_ralph-supervisor
```

### Windows (Service Control Manager)

```powershell
radioactive_ralph service install
```

Registers a service via SCM. Requires an elevated terminal. Verify:

```powershell
Get-Service radioactive_ralph-supervisor
```

## 2. Status, list, uninstall

```sh
radioactive_ralph service status      # report this machine's installed-service state
radioactive_ralph service uninstall   # remove the service definition
```

`service status` reports the resolved backend (launchd/systemd/SCM) and
whether it's installed, plus the unit path. Uninstalling removes only the
OS-service registration; the binary and the user-level database are
untouched.

## 3. Logs

### Foreground

```sh
radioactive_ralph --supervisor --log-format json 2> ~/tmp/ralph.log
```

`--log-format json` emits one structured record per lifecycle/reaper
event — easier to grep/assert on than free-form text; `--log-format text`
(the default) is more readable interactively.

### Installed service

- macOS launchd: `launchctl list` for status; logs land wherever the
  generated plist directs stdout/stderr (check the plist for the path).
- Linux systemd: `journalctl --user -u radioactive-ralph -f`
- Windows SCM: Event Viewer → Windows Logs → Application, filter source
  `radioactive-ralph`

## 4. Stale state recovery

When a client says "no supervisor is running" but you expect one, the
previous supervisor process crashed without cleaning its socket and
heartbeat file.

### 4a. Verify the supervisor is actually dead

```sh
pgrep -f "radioactive_ralph --supervisor" || echo "no orphan"
```

If `pgrep` shows a PID, the supervisor is still alive — stop it (`kill
<pid>`, or the OS service manager's stop) first, then re-check.

### 4b. Remove the stale socket and heartbeat file manually

The stale endpoint lives directly under your XDG state root (there is no
per-repo subdirectory at this layer — the supervisor is one process per
machine, not per repo):

```sh
# macOS
rm -f "$HOME/Library/Application Support/radioactive-ralph/service.sock"
rm -f "$HOME/Library/Application Support/radioactive-ralph/service.sock.alive"

# Linux / WSL2
rm -f "${XDG_STATE_HOME:-$HOME/.local/state}/radioactive-ralph/service.sock"
rm -f "${XDG_STATE_HOME:-$HOME/.local/state}/radioactive-ralph/service.sock.alive"
```

On Windows, named pipes are cleaned by reboot; if a reboot is
impractical, restart the SCM service instead.

Then `radioactive_ralph --supervisor` (or the OS service manager) will
succeed again — a fresh supervisor also self-reclaims a stale socket
automatically at startup if the recorded PID is dead, so manual removal
is a fallback, not the primary recovery path.

## When something goes wrong

- Socket missing or dead → [Troubleshooting → dead
  socket](./troubleshooting.md#dead-socket-or-pipe)
- Service-install fails → [Troubleshooting → service-install
  errors](./troubleshooting.md#service-install-errors)
- Platform-specific caveats → [Platform notes](./platforms.md)
