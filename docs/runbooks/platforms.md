---
title: Platform notes
description: macOS launchd, Linux systemd-user, and native Windows foreground and named-pipe caveats.
lastUpdated: 2026-07-26
---

The supervisor is a single Go binary, but the OS integration surface
differs per platform. This page collects the caveats that bite in
practice. macOS and Linux have exactly one supervisor service per user per
machine — not one per repo. Native Windows offers only a limited foreground
control plane in v0.22; WSL2 is the functional provider-backed route.

## macOS (launchd)

### LaunchAgent vs. LaunchDaemon

radioactive-ralph installs a **LaunchAgent** (per-user), not a
LaunchDaemon (system-wide). Agents run at login under your user and can
access your keychain — which means they can invoke `claude`/`codex`/
`opencode` CLIs that authenticated under your account. Daemons run as
root and would need separate auth.

### Plist location

```
~/Library/LaunchAgents/jbcom.radioactive-ralph.supervisor.plist
```

### Cannot launch because macOS is asleep

launchd won't fire a plist under `RunAtLoad` if the machine sleeps
before login completes. On sleep-heavy laptops, expect the supervisor to
appear "stopped" after a cold boot. Fix:
`launchctl kickstart -k gui/$UID/jbcom.radioactive-ralph.supervisor` or
log out/in.

### SIP / code-signing

We don't code-sign the binary (v1). The first time you run it,
Gatekeeper will complain. Fix:

```sh
xattr -d com.apple.quarantine $(which radioactive_ralph)
```

Or right-click → Open in Finder on the binary once.

## Linux (systemd --user)

### User bus vs. system bus

We install a **user unit**, not a system unit. Requires `systemd --user`
to be running — i.e. you're in a graphical session or you enabled
linger:

```sh
loginctl enable-linger $USER
```

Without linger, the user bus dies on logout and takes the unit with it.

### Unit location

```
~/.config/systemd/user/radioactive_ralph-supervisor.service
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

We ship no profile; the socket lives under
`$HOME/.local/state/radioactive-ralph/` by default.

## Windows

### Named-pipe endpoint

On Windows the discovery endpoint is a named pipe, not a Unix socket:

```
\\.\pipe\radioactive_ralph-<token>-service
```

`<token>` is a short hash of the state-root path, not a repo slug. Each user
account has one supervisor by default, so the token disambiguates distinct
per-user `RALPH_STATE_DIR` overrides (including tests), not repos. A foreground
supervisor running as the normal user creates a pipe bound to that user's SID.

The rejected SCM design ran as `LocalSystem` and granted
`GenericRead+GenericWrite` to broad `WinInteractiveSid` so an interactive
client could reach it. That authorization is unsafe for Ralph's mutating
control API and is one reason native SCM support is disabled.

### SCM install/start is disabled

```powershell
radioactive_ralph --supervisor
```

v0.22 supports native Windows foreground supervisor/client control-plane
execution, not provider workers or SCM persistence. Worker startup returns
`ErrPTYUnsupported`. `service install` and service start fail closed before
mutation even from an elevated terminal. `service status` and
`service uninstall` exist only to inspect and remove a prior development
registration. Do not start it.

SCM support can return only with an identity-bound per-user service, secure
binary/config/state ACLs, a pipe authorized to the exact user SID, a real
native worker pty and provider-backed repository turn under that identity, and
clean native install/start/status/uninstall end-to-end proof. See the
[accepted safety contract](../superpowers/specs/2026-07-26-windows-scm-safety-disable-design.md).

### Pipes die on reboot

Windows named pipes are per-session objects; they don't persist across
reboots. This is normal — the foreground supervisor recreates the pipe on
start.

### Windows Defender / SmartScreen

First run may trigger a SmartScreen warning. Fix: right-click the binary
→ Properties → Unblock. Or sign the binary (v1 doesn't).

### Windows CI vs. native Windows

The CI smoke test (`.github/workflows/ci.yml`, Windows job) runs the limited
foreground supervisor/client lifecycle on a GitHub-hosted runner and asserts
the unsupported worker boundary. It does not prove SCM safety or native
provider execution. It's sensitive to:

- Process exit races — poll `HasExited`, not `Wait-Process` (which
  throws when the PID is already gone)
- Named-pipe name collisions between parallel CI jobs — the pipe name
  includes a per-job random suffix in test mode
- Long-running workers that exceed the default job timeout — keep
  integration tests under 2 minutes

If a Windows CI flake doesn't reproduce on a real Windows machine, compare
native evidence before classifying it. Hosted-runner unit and foreground
smokes are not substitutes for the native pty/provider and clean SCM
end-to-end required to re-enable service support.

## WSL2

WSL2 is "Linux on Windows" from the binary's perspective — install the
Linux tarball, run the Linux systemd integration. Two caveats:

- WSL1 is **not** supported. systemd doesn't run on WSL1.
- Cross-filesystem ops (a project on the Windows disk via `/mnt/c/...`)
  are slow. Keep projects on the WSL filesystem (`~/src/`) for
  responsiveness.

## Docker / containers

Untested in v1. The binary runs in Alpine + glibc containers, but the
OS-service integration (launchd/systemd; native Windows SCM is disabled)
doesn't. Native Windows foreground remains control-plane-only; use WSL2 for
functional provider-backed execution.
