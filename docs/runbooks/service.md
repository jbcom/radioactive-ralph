---
title: Supervisor service install, start, stop, recover
description: Manage the supervisor as a per-user OS service on macOS and Linux.
lastUpdated: 2026-07-26
---

`radioactive_ralph service ...` manages the supervisor
(`radioactive_ralph --supervisor`) as a per-user auto-restarting OS
service on macOS and Linux. The supervisor is the one long-lived process on
the machine — there is one per user, not one per repo.

> [!IMPORTANT]
> v0.22 intentionally disables native Windows SCM install/start. `LocalSystem`
> cannot safely represent Ralph's user-scoped state and credentials, and the
> prior filesystem and broad interactive-user pipe model is not an acceptable
> privilege boundary. Native foreground `radioactive_ralph --supervisor` is a
> limited control-plane path because provider worker ptys are unsupported; use
> WSL2 with `systemd --user` for functional execution. See the
> [Windows SCM safety contract](../superpowers/specs/2026-07-26-windows-scm-safety-disable-design.md).

## service install vs. running --supervisor directly

| Command | What it does | When to use |
|---------|--------------|-------------|
| `radioactive_ralph --supervisor` | Runs the supervisor **in the foreground** | First-run debugging, CI smoke tests, watching logs directly |
| `radioactive_ralph service install` | Installs or reloads the macOS/Linux definition and starts the supervisor now | Daily use on macOS/Linux; the supervisor auto-starts at login and auto-restarts on crash |

## 1. Install as an OS service

There is exactly one service definition per user per machine — not one
per repo — named `jbcom.radioactive-ralph.supervisor` (launchd) or
`radioactive_ralph-supervisor` (systemd).

### macOS (launchd)

```sh
radioactive_ralph service install
```

Writes `~/Library/LaunchAgents/jbcom.radioactive-ralph.supervisor.plist`
and bootstraps/restarts it. Re-running the command reloads changed binary
or environment settings rather than leaving launchd's cached definition
active. The command does not report success until the supervisor endpoint
answers. Verify:

```sh
launchctl list | grep radioactive-ralph
```

### Linux (systemd --user)

```sh
radioactive_ralph service install
```

Writes `~/.config/systemd/user/radioactive_ralph-supervisor.service` and
reloads, enables, and starts it. The command does not report success until
the supervisor endpoint answers. Verify:

```sh
systemctl --user status radioactive_ralph-supervisor
```

### Bound worker concurrency

Pass supervisor environment at install time to set an operator-chosen
emergency ceiling for simultaneous provider turns:

```sh
# Replace N with an operator-chosen positive-integer emergency ceiling.
radioactive_ralph service install --env RALPH_MAX_PARALLEL=N
```

The generated service definition persists the value across login and
restart. Valid values are `1` through `256`; values outside that range are
rejected before an existing service is changed. An explicitly empty or
whitespace-only value is invalid; only an absent variable selects the
compatibility default. The range is validation, not an operating
recommendation. Unset selects the pre-ceiling unbounded behavior preserved by
v0.22, which is neither adaptive nor recommended as an optimum. Re-run
`service install` with the intended environment whenever changing the bound.

`service install` considers the Ralph executable directory first, then
inherited and platform-standard candidates, retaining accepted entries in that
order as a de-duplicated execution `PATH`. Unix rejects an entire entry when
any lexical component is relative, missing, non-directory,
symlinked, foreign-owned, or group/other-writable. macOS additionally
recognizes the architecture-specific exact Homebrew path
(`/opt/homebrew/bin` on Apple Silicon or `/usr/local/bin` on Intel) when its
root/current-user ownership, privileged `wheel`/`admin` group metadata, and
trusted fixed ancestry are intact. These narrow exceptions permit Homebrew's
normal group-writable bin directory without weakening unrelated inferred
entries. This is required because launchd's default path omits Homebrew and
`~/.local` agent CLIs. Architecture here means the physical host: an `amd64`
Ralph process translated by Rosetta uses Apple's `sysctl.proc_translated`
signal and selects `/opt/homebrew/bin`. If that signal cannot be read, Ralph
keeps the process architecture and does not grant the Apple Silicon exception.
The `/opt/homebrew/bin` leaf may be owned by root or the effective user, but
must retain the trusted `admin` group and must not be world-writable. The
dedicated service-manager smoke pins `macos-15` for ARM and
`macos-15-intel` for Intel so a floating runner image cannot silently change
this architecture contract. An explicit `--env PATH=...` is an
operator-controlled override and is not filtered:

```sh
radioactive_ralph service install \
  --env PATH=/controlled/provider/bin:/usr/bin:/bin
```

Symlinked Homebrew `opt` paths are intentionally not inferred. To pin
service-side scripts to Node 24, first validate the local Homebrew installation
and then use the explicit override, for example:

```sh
radioactive_ralph service install \
  --env PATH=/opt/homebrew/opt/node@24/bin:/opt/homebrew/bin:/usr/bin:/bin
```

### Native Windows (SCM intentionally disabled)

Do not elevate and retry `service install`. v0.22 rejects native Windows SCM
installation and service start before changing the service definition or
writing its configuration.

For native control-plane inspection, run the supervisor as the interactive
user:

```powershell
radioactive_ralph --supervisor
```

That foreground process uses the same user's Ralph state and SID-bound named
pipe, but provider worker dispatch fails with `ErrPTYUnsupported`. It is not a
functional native agent runtime. For provider-backed operation on a Windows
machine, use WSL2 and the Linux `systemd --user` instructions.

`service status` and `service uninstall` remain remediation operations only
for a registration matching Ralph's historical executable, marker arguments,
service metadata, and exact AppData `UnitPath` for the resolved user home. An
unknown service using the same name is reported as an ownership error and is
never stopped or deleted. Do not start a prior Ralph service. The
[accepted safety contract](../superpowers/specs/2026-07-26-windows-scm-safety-disable-design.md#re-enable-gate)
lists the identity, ACL, pipe authorization, provider-access, and clean native
end-to-end proof required before SCM support can return.

## 2. Status, list, uninstall

```sh
radioactive_ralph service status      # report this machine's installed-service state
radioactive_ralph service uninstall   # stop the service, then remove its definition
```

`service status` reports the resolved backend and whether it is installed,
plus the unit path. On native Windows it is inspection-only and identifies a
prior unsafe Ralph registration only after validating its historical
ImagePath; an unknown same-name registration returns an error. Uninstalling
first stops the validated supervisor, re-validates ImagePath, then removes the
OS-service registration; the binary and the user-level database are untouched.

> [!WARNING]
> Re-running `service install` reconciles the definition by restarting the
> invoking user's single supervisor. A restart terminates supervisor-owned
> provider processes and interrupts in-flight workers. Pause or finish active
> plans before changing the service binary or environment.

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
- Native Windows foreground: keep the terminal open or redirect stderr with
  `--log-format json`. Native SCM execution is disabled.

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

On Windows, named pipes are cleaned when the foreground process exits or at
reboot. Start `radioactive_ralph --supervisor` again as the same interactive
user; do not start a prior SCM registration.

Then `radioactive_ralph --supervisor` (or the supported macOS/Linux service
manager) will succeed again — a fresh supervisor also self-reclaims a stale
socket automatically at startup if the recorded PID is dead, so manual
removal is a fallback, not the primary recovery path.

## When something goes wrong

- Socket missing or dead → [Troubleshooting → dead
  socket](./troubleshooting.md#dead-socket-or-pipe)
- Service-install fails → [Troubleshooting → service-install
  errors](./troubleshooting.md#service-install-errors)
- Platform-specific caveats → [Platform notes](./platforms.md)
