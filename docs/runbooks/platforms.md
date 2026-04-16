---
title: Platform notes
description: macOS launchd, Linux systemd-user, and Windows SCM + named-pipe caveats.
---

The repo-scoped runtime is a single Go binary, but the OS integration
surface differs per platform. This page collects the caveats that
bite in practice.

## macOS (launchd)

### LaunchAgent vs LaunchDaemon

radioactive-ralph installs a **LaunchAgent** (per-user), not a
LaunchDaemon (system-wide). Agents run at login under your user and
can access your keychain — which means they can invoke `claude` /
`codex` / `gemini` CLIs that authenticated under your account.
Daemons run as root and would need separate auth.

### Plist location

```
~/Library/LaunchAgents/com.jbcom.radioactive-ralph.<repo-slug>.plist
```

`<repo-slug>` is derived from the repo path to disambiguate
multi-repo operators. One agent per repo.

### Cannot launch because macOS is asleep

launchd won't fire a plist under `RunAtLoad` if the machine sleeps
before login completes. On sleep-heavy laptops, expect the service to
appear "stopped" after a cold boot. Fix: `launchctl kickstart -k
gui/$UID/com.jbcom.radioactive-ralph.<slug>` or just log out/in.

### SIP / code-signing

We don't code-sign the binary (v1). The first time you run it,
Gatekeeper will complain. Fix:

```sh
xattr -d com.apple.quarantine $(which radioactive_ralph)
```

Or right-click → Open in Finder on the binary once. Post-launch,
subsequent invocations are fine.

## Linux (systemd --user)

### User bus vs system bus

We install a **user unit**, not a system unit. Requires `systemd
--user` to be running — i.e. you're in a graphical session or you
enabled linger:

```sh
loginctl enable-linger $USER
```

Without linger, the user bus dies on logout and takes the unit with
it.

### Unit location

```
~/.config/systemd/user/radioactive-ralph-<repo-slug>.service
```

### `XDG_RUNTIME_DIR` missing

Under SSH without `loginctl enable-linger`, `systemctl --user` fails
with `Failed to connect to bus`. Set:

```sh
export XDG_RUNTIME_DIR=/run/user/$UID
```

Or use linger.

### AppArmor / SELinux

If the binary fails to open the Unix socket, check kernel audit logs:

```sh
sudo journalctl -u apparmor -n 50
sudo ausearch -m AVC -ts recent
```

We ship no profile; fall back to writing the socket under
`$HOME/.local/state/radioactive-ralph/` if the confinement is tight.

## Windows

### Named-pipe endpoint

On Windows the control plane is a named pipe, not a Unix socket:

```
\\.\pipe\radioactive-ralph-<repo-slug>
```

Pipes are scoped to the current user's session. That means:

- A service installed under `LocalSystem` (SCM) creates a pipe that
  interactive users can connect to (we explicitly grant
  `GenericRead+GenericWrite` to `WinInteractiveSid` in the DACL; see
  `internal/ipc/transport_windows.go`)
- A service installed under your normal user account creates a pipe
  that only that user can connect to (DACL grants `GenericAll` to
  the user's SID only)

### SCM install requires admin

```powershell
# Elevated PowerShell required
radioactive_ralph service install
```

Non-elevated terminals get `access denied` and no service is
registered.

### Pipes die on reboot

Windows named pipes are per-session objects; they don't persist
across reboots. This is normal. The installed service recreates the
pipe on start. Don't confuse a post-reboot "pipe does not exist" error
with a bug.

### Windows Defender / SmartScreen

First run may trigger SmartScreen warning ("Windows protected your
PC"). Fix: right-click the binary → Properties → Unblock. Or sign the
binary (v1 doesn't).

### PowerShell execution policy

If scripts in `.radioactive-ralph/` scripts fail with "running scripts
is disabled on this system":

```powershell
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
```

(One-time, per-user.)

### Windows CI vs native Windows

The CI smoke test (`.github/workflows/ci.yml` `Test (windows-latest)`)
runs the service lifecycle on a GitHub-hosted runner. It's sensitive
to:

- Process exit races — see the `HasExited` poll in the smoke script
  (not `Wait-Process`, which throws when the PID is already gone)
- Named-pipe name collisions between parallel CI jobs — the pipe name
  includes a per-job random suffix in test mode
- Long-running workers that exceed the default job timeout — keep
  integration tests under 2 minutes

If a Windows CI flake doesn't reproduce on a real Windows machine,
suspect hosted-runner instability and rerun before investigating.

## WSL2

WSL2 is "Linux on Windows" from the binary's perspective — install
the Linux tarball, run the Linux systemd integration. Two caveats:

- WSL1 is **not** supported. Systemd doesn't run on WSL1.
- Cross-filesystem ops (repo on Windows disk via `/mnt/c/...`) are
  slow. Keep repos on the WSL filesystem (`~/src/`) for the service
  to stay responsive.

## Docker / containers

Untested in v1. The binary runs in Alpine + glibc containers, but
the OS-service integration (launchd/systemd/SCM) doesn't — use
`service start` in the foreground instead. Future work: a systemd-in-
container or tini-managed container mode.
