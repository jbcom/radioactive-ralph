# `packaging/wsl/` — the bundled `radioactive-ralph` WSL2 distro

Produces `rootfs.tar.gz`, the tarball `gowsl.Distro.Register()` imports to create the dedicated
WSL2 distro Windows dispatch runs provider CLI turns in. Full architecture and rationale:
`docs/superpowers/specs/2026-08-20-windows-wsl2-dispatch-design.md`.

## Why this exists

Native Windows cannot run a provider CLI turn correctly: `creack/pty` has no Windows
implementation, and ConPTY (OS-shipped or the official `conpty.dll` fix) cannot deliver a clean
stdin EOF to a hosted child — confirmed as a real, open upstream limitation, not something this
project can fix in its own code. The working fix is dispatching through `wsl.exe` as a plain
`os/exec` subprocess, which has correct Unix pipe/EOF semantics. This directory builds the distro
that runs on the other end of that dispatch.

## Building

Requires Docker (a release-pipeline dependency, never an install-time one for end users), and on
Windows specifically requires **Git-Bash or WSL** to run `build-rootfs.sh` at all — the script's
shebang is `#!/usr/bin/env bash` and it uses `pwd -W` (a Git-Bash-specific extension) for the
Docker volume-mount path; plain PowerShell/cmd cannot run it:

```bash
./build-rootfs.sh
```

Produces `rootfs.tar.gz` in this directory. Not committed — built fresh per release and attached
as a Windows release asset alongside the existing 4-variant Windows build matrix (see
`packaging/README.md`).

## What's in the image, and what deliberately isn't

- `systemd` as PID 1 (`wsl.conf`'s `[boot] systemd=true`), with the specific units Microsoft's own
  [custom-distro guidance](https://learn.microsoft.com/en-us/windows/wsl/build-custom-distro)
  documents as WSL2-incompatible masked in the `Dockerfile`.
- WSL interop explicitly enabled (`[interop] enabled=true appendWindowsPath=true`) — this is
  load-bearing, not just the default: it's what would let a turn invoke the already-authenticated
  Windows-side `claude`/`codex`/`copilot` binaries directly, instead of requiring a separate
  Linux-side install and re-authentication (still needs its own empirical validation pass, see the
  design spec's open questions).
- **Not** true Google "distroless": distroless is deliberately shell-less, init-less, and
  package-manager-less (single static binary only), which is incompatible with needing systemd and
  arbitrary provider-CLI shell dispatch. The `Dockerfile` instead applies the same hygiene through
  a genuine multi-stage build — a builder stage does all `apt` work, and only the resulting
  minimized filesystem (no apt/dpkg, docs, man pages, apt lists) survives into the final stage.
- **Not** provider CLIs (claude/codex/opencode). Whether they run via WSL interop against the
  Windows-side install or get a Linux-side install added later is an open question pending
  validation — the image doesn't guess.

## Size

Built and measured 2026-08-20: **68MB compressed**. `dive` shows 0 wasted bytes / 100% layer
efficiency (the multi-stage build already means there's nothing cross-layer to reclaim); `du`
inside the image shows where the 193MB uncompressed footprint actually goes:
`/usr/lib/x86_64-linux-gnu` (92MB — glibc and systemd's own runtime deps, the practical floor for
any systemd-capable image) and `/usr/lib/git-core` (25MB). Both are deliberate keeps, not
oversights: systemd is a hard requirement, and git is kept because whether providers ever need to
run git operations from inside this distro is still an open question in the design spec — cutting
it now on a guess would be premature. For comparison, a typical Ubuntu WSL rootfs runs
200–600MB+; 68MB for a real systemd-capable Debian image is already lean.

## Registering it

```go
distro := gowsl.NewDistro(ctx, "radioactive-ralph")
if err := distro.Register("rootfs.tar.gz"); err != nil { ... }
```

`gowsl` (`github.com/ubuntu/gowsl`, cloned to `Reference-Repos/GoWSL` for reference) is used for
distro lifecycle management only — import/register/state/unregister. **Not** for command
execution: `Distro.Command()` (`WslLaunch` under the hood) was tested and reproduces the exact
same broken-stdin-EOF symptom as ConPTY. Actual provider dispatch uses plain Go `os/exec` spawning
`wsl.exe -d radioactive-ralph -- <cmd>`, which was verified to round-trip cleanly (write → close →
child-observed EOF → clean exit) — see the design spec for the full evidence trail.
