---
title: Service install, start, stop, recover
description: Manage the repo-scoped runtime as a durable service on macOS, Linux, and Windows.
---

`radioactive_ralph service ...` is the operator-facing control plane
for the durable repo runtime. The runtime is a single repo-scoped
process that watches the plan DAG and dispatches ready tasks to the
configured provider(s).

## service start vs service install

| Command | What it does | When to use |
|---------|--------------|-------------|
| `service start` | Runs the service **in the foreground** | First-run debugging, CI smoke tests, situations where you want to see logs directly |
| `service install` | Registers the service with your OS service manager (launchd / systemd / SCM) | Daily use; the service auto-starts at login |

You only need to run one of these. `install` is for durable use;
`start` is for iterative debugging.

## 1. Install as an OS service

### macOS (launchd)

```sh
radioactive_ralph service install
```

Emits `~/Library/LaunchAgents/com.jbcom.radioactive-ralph.<repo-slug>.plist`
and loads it via `launchctl bootstrap`. Verify:

```sh
launchctl list | grep radioactive-ralph
```

### Linux (systemd --user)

```sh
radioactive_ralph service install
```

Emits `~/.config/systemd/user/radioactive-ralph-<repo-slug>.service`
and enables + starts the unit. Verify:

```sh
systemctl --user status radioactive-ralph-<repo-slug>
```

### Windows (Service Control Manager)

```powershell
radioactive_ralph service install
```

Registers a service via SCM. Requires admin (run the terminal elevated).
Verify:

```powershell
Get-Service radioactive-ralph-<repo-slug>
```

## 2. Start / stop / restart

```sh
radioactive_ralph service start       # foreground (no install needed)
radioactive_ralph stop                # graceful shutdown of the running service
```

For the installed service, use the OS service manager:

```sh
launchctl kickstart -k  gui/$UID/com.jbcom.radioactive-ralph.<slug>   # macOS
systemctl --user restart radioactive-ralph-<slug>                      # Linux
Restart-Service radioactive-ralph-<slug>                               # Windows
```

## 3. Uninstall

```sh
radioactive_ralph service uninstall
```

Removes the plist/unit/service registration. The binary + your
`.radioactive-ralph/` config stay put; only the OS-service hook is
removed.

## 3b. List + status

```sh
radioactive_ralph service list      # enumerate installed repo service units
radioactive_ralph service status    # report the current repo's service-unit state
```

`service list` enumerates every installed repo service unit on the
machine. `service status` reports the installed-service state for the
current repo (loaded/stopped/failed on launchd, active/inactive/failed
on systemd, Running/Stopped on Windows SCM).

## 4. Stale state recovery

When `radioactive_ralph status` says the service is running but
nothing responds, the previous service process crashed without
cleaning its control-plane socket (or named pipe on Windows) and
heartbeat file.

### 4a. Verify the service is actually dead

```sh
pgrep -f "radioactive_ralph service" || echo "no orphan"
```

If `pgrep` shows a PID, the service is still alive — use
`radioactive_ralph stop` (or `kill <pid>`) first, then re-check.

### 4b. Remove the stale socket and heartbeat file manually

`radioactive_ralph` does not ship a `service clean` subcommand. The
stale control-plane endpoint lives next to the heartbeat file under
your XDG state root. Remove both by hand:

```sh
# macOS
rm -f "$HOME/Library/Application Support/radioactive-ralph/<repo-hash>"/control.sock
rm -f "$HOME/Library/Application Support/radioactive-ralph/<repo-hash>"/control.alive

# Linux / WSL2
rm -f "${XDG_STATE_HOME:-$HOME/.local/state}/radioactive-ralph/<repo-hash>"/control.sock
rm -f "${XDG_STATE_HOME:-$HOME/.local/state}/radioactive-ralph/<repo-hash>"/control.alive
```

On Windows, named pipes are cleaned by reboot; if a reboot is
impractical, restart the SCM service.

Then `service start` (or the OS service manager) will succeed again.

## 5. Logs

### Foreground

`service start` writes logs to stderr. Redirect:

```sh
radioactive_ralph service start 2> ~/tmp/ralph.log
```

### Installed service

- macOS launchd: `~/Library/Logs/radioactive-ralph-<slug>.log`
- Linux systemd: `journalctl --user -u radioactive-ralph-<slug> -f`
- Windows SCM: Event Viewer → Windows Logs → Application, filter
  source `radioactive-ralph`

## When something goes wrong

- Control-plane socket missing or dead → [Troubleshooting → dead socket](./troubleshooting.md#dead-socket-or-pipe)
- Service-install fails → [Troubleshooting → service-install errors](./troubleshooting.md#service-install-errors)
- Platform-specific caveats → [Platform notes](./platforms.md)
