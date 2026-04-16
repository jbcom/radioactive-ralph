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

## 4. Stale state recovery

When `radioactive_ralph status` says the service is running but
nothing responds:

### 4a. Check the heartbeat

The runtime writes a heartbeat file next to the control-plane socket
(or named pipe on Windows). If its mtime is older than ~10s, the
service is dead but left its socket behind.

```sh
ls -l $(radioactive_ralph status --socket-path 2>&1 | grep heartbeat)
```

### 4b. Clean the dead socket

```sh
radioactive_ralph service clean
```

Removes the stale control-plane endpoint (Unix socket or Windows
named-pipe reference) and the heartbeat file. Then `service start`
will succeed again.

### 4c. Verify no orphan process

```sh
pgrep -f "radioactive_ralph service" || echo "no orphan"
```

If `pgrep` shows a PID, the service is still alive — don't `service
clean`; use `radioactive_ralph stop` (or `kill <pid>`) first.

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
