# Native Windows provider dispatch via a managed WSL2 distro

Status: proposed, 2026-08-20. Supersedes the "provider workers are categorically unsupported on
native Windows" framing in
[Windows SCM safety disable](./2026-07-26-windows-scm-safety-disable-design.md) and the original
[supervisor architecture design](./2026-07-16-supervisor-architecture-design.md) — **only** for
the provider-dispatch question. This spec does not touch the SCM/service-install safety gate,
which stays exactly as-is (disabled, re-enable criteria unchanged).

## Background: why ConPTY was tried and abandoned

`ErrPTYUnsupported` exists today because `creack/pty` has no Windows implementation. The obvious
fix looked like native ConPTY (`CreatePseudoConsole`) support. A same-day investigation (full
evidence trail in a `radioactive-ralph-conpty-investigation` note, not part of this repo)
established, empirically, on a real Windows host:

- ConPTY process launch + output streaming genuinely works (`CreatePseudoConsole` +
  `CreateProcess` with `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE`, via `golang.org/x/sys/windows`, no
  new dependency).
- ConPTY **cannot deliver a clean stdin EOF to a hosted child.** Confirmed as a known, still-open
  upstream limitation via a Microsoft Windows Terminal engineer's own comment on
  [microsoft/terminal#15006](https://github.com/microsoft/terminal/discussions/15006). Confirmed
  **directly**, not just by reading the discussion: downloading and loading the official
  `Microsoft.Windows.Console.ConPTY` NuGet package's `conpty.dll`/`OpenConsole.exe` (the
  engineer's own recommended fix) reproduced the *identical* failure — write delivered nowhere,
  close-of-stdin never observed as EOF by the child, across three independent runtimes (Go,
  Node.js, and a native Win32 console tool), with delays up to 20 seconds.
- This directly blocks `internal/provider/claude.go`'s `OneShotInput` protocol (used for *every*
  claude-provider turn, not an edge case), and would block any provider using the same pattern.

Conclusion: ConPTY, OS-shipped or the officially "fixed" redistributable, is not viable for this
tool's one-shot, non-interactive turn model. Do not re-attempt it without new evidence.

## The actual fix: dispatch through `wsl.exe`, not ConPTY

Empirically verified, same investigation, on the same host:

1. Plain shell redirection into WSL (`printf ... | wsl.exe -d <distro> -- cat`) round-trips
   cleanly: data delivered, EOF observed, clean exit.
2. **`github.com/ubuntu/gowsl`'s own `Distro.Command()` API (`WslLaunch` under the hood)
   reproduces ConPTY's exact failure mode** — data delivered, but `stdin.Close()` never propagates
   as EOF within 20s. So "use GoWSL for execution" is not the fix.
3. **Plain Go `os/exec` spawning `wsl.exe` as an ordinary Windows child process** (real
   Windows-native anonymous pipes via `cmd.StdinPipe()`/`cmd.StdoutPipe()`, exactly like any other
   subprocess on any platform — no ConPTY, no CGO, no GoWSL) reproduced case (1)'s clean behavior
   on the first try: write → close → child observes EOF → child exits → `cmd.Wait()` returns nil.

The fix is specifically **#3**: on Windows, a provider turn is dispatched as
`exec.Command("wsl.exe", "-d", <ralph-distro>, "--", <provider CLI invocation>)`, using the exact
same `os/exec`-based plumbing pattern this codebase already trusts on every other platform. No new
pty abstraction, no ConPTY, no CGO.

## Proposed architecture

### A dedicated, managed WSL2 distro (Docker Desktop's pattern)

Docker Desktop, Rancher Desktop, and Podman Desktop all converge on the same shape: `wsl --import`
a minimal, purpose-built rootfs into a dedicated, distinctly-named distro (`docker-desktop`,
`rancher-desktop`, a Podman machine), driven from the native Windows process, not exposed as the
user's interactive shell. Ralph should do the same: a `radioactive-ralph` (name TBD) distro,
auto-provisioned on first native-Windows dispatch, hidden from normal interactive use (matching
Docker's approach — no special "hidden" flag exists; it's just undocumented/unconfigured as an
interactive default).

- **Provisioning**: `github.com/ubuntu/gowsl`'s `Import`/`Register` (MIT-licensed, actively
  maintained by Canonical, already the dependency `canonical/ubuntu-pro-for-wsl` uses for the same
  kind of distro lifecycle management) — for import/register/state/unregister only, **not** for
  command execution (see above).
- **Rootfs**: built from `packaging/wsl/Dockerfile`, a multi-stage build (builder stage does all
  `apt` work; final stage is only the minimized filesystem copied onto `scratch` — not true
  Google "distroless", which is deliberately shell-less/init-less/package-manager-less and
  therefore incompatible with needing systemd as PID 1 and dispatching arbitrary provider-CLI
  shell commands, but applying the same hygiene: no apt/dpkg, docs, man pages, or apt lists in the
  final image). `packaging/wsl/wsl.conf` enables `systemd` and explicit WSL interop settings (see
  below), with the specific systemd units Microsoft documents as WSL2-incompatible masked in the
  Dockerfile. `packaging/wsl/build-rootfs.sh` produces `rootfs.tar.gz` following Microsoft's
  documented method exactly (`docker export` + `tar --numeric-owner --absolute-names | gzip
  --best` — [Build a Custom Linux Distribution for WSL](https://learn.microsoft.com/en-us/windows/wsl/build-custom-distro)),
  ready for `gowsl.Distro.Register()`. Does not use the `.wsl`-file/manifest/OOBE machinery that
  guide also covers — that's for end-user-installable distros (`wsl --install`); Ralph's distro is
  hidden and programmatically managed, matching Docker/Rancher/Podman's own distros, none of which
  go through that path either.
- **Prerequisite handling**: match Docker's approach — assume WSL2 is already enabled (real
  precedent: this exact host already has WSL2 fully functional via Docker Desktop's own
  `docker-desktop` distro), guide the user if it isn't, don't attempt silent `dism`/feature
  self-enablement.

### Dispatch path

`internal/agent`'s Windows build gets a new `startPTY`-equivalent that, instead of allocating any
pty at all, execs `wsl.exe -d <distro> -- <cmd>` via plain `os/exec`, wired through the existing
`Agent` lifecycle exactly the way any other platform's subprocess is. No pty abstraction change is
needed in the shared `agent.go` — this sidesteps the `ptyFile` interface refactor an earlier,
abandoned ConPTY attempt required, because there is no pty here at all, just ordinary process I/O.

### Windows-side authentication carries over via WSL interop (confirmed 2026-08-20)

Verified directly against a real, disposable `wsl.exe --import` of this repo's own
`packaging/wsl/rootfs.tar.gz` (imported, tested, `--unregister`'d — not the release-managed
distro): WSL2 interop registers Windows PE binaries in `binfmt_misc` (`WSLInterop`, magic `4d5a`),
and `/mnt/c/...` is mounted per `wsl.conf`'s `[automount]`, so a Windows-side, already-authenticated
`claude`/`copilot` install genuinely is reachable from inside the distro without a separate
Linux-side install or re-auth. `/mnt/c/Windows/System32/cmd.exe /c "echo ..."` and a piped-stdin
`findstr .` both round-tripped cleanly (data delivered, clean EOF, exit 0) — the same
plain-pipe/no-ConPTY semantics this design's Windows→WSL direction already relies on, just in
reverse.

**Real gotcha, found empirically, not by reasoning**: `claude`/`copilot`'s actual Windows CLI
entry point is an npm-generated `.cmd` shim (a plain batch script), not a PE `.exe` — and
`binfmt_misc` only recognizes the `MZ` PE header. Invoking a `.cmd` shim's `/mnt/c/...` path
*directly* from inside WSL does **not** invoke it as a Windows program at all; the Linux shell
tries to interpret the batch script as a POSIX shell script instead, and **fails silently with
exit code 0**, emitting garbage (`@ECHO: not found`, etc.) that looks superficially like output.
This is a landmine for any dispatch code that shells out to a bare command name/path expecting
`binfmt_misc`/`appendWindowsPath` to do the right thing. The actual working invocation routes
through `cmd.exe /c "<path> <args>"` explicitly (confirmed: printed the real installed
`2.1.237 (Claude Code)` version, not a stub) — any future interop-dispatch provider path must do
the same, never a bare exec of the `.cmd` path.

Not yet built: an actual provider-dispatch mode that uses this (today's `internal/agent` Windows
path only dispatches *into* the distro; it does not yet reach back out to Windows-side CLIs from
inside it). That's a distinct, separable feature — the open question below is now "how", not
"whether".

## What this does NOT change

- The Windows SCM safety gate (`service install`/`start` disabled) is untouched. This spec is
  entirely about the **foreground** control plane's dispatch capability
  (`radioactive_ralph --supervisor` run manually in an open terminal), which does not have the
  SCM identity/privilege-escalation problem that gate exists for.
- `internal/contain` (kernel write-containment) still reports "not needed" on native Windows for
  the *Windows-side* process (the dispatch is a subprocess exec, not a contained provider run);
  containment inside the WSL2 distro is a separate, later question, not blocking this design.

## Open questions before implementation

1. How `rootfs.tar.gz` is versioned and shipped in Ralph's own release pipeline (built once per
   release and attached as a Windows-only release asset, most likely, alongside the existing
   4-variant Windows build matrix — not built on end-user machines, which would require Docker as
   an install-time dependency it should never become).
2. Whether `wsl.exe`'s path/availability detection needs its own `doctor` check distinct from the
   existing "service platform" WARN.
3. How the supervisor decides *when* to provision the distro (first native-Windows dispatch
   attempt vs. eagerly at `--init`).
4. Interop-based Windows-side CLI invocation is now confirmed viable (see above, including the
   `.cmd`-shim/`cmd.exe` gotcha) but not yet implemented as an actual dispatch mode — needs its
   own design/PR to decide whether it's a `provider` option, a doctor-detected fallback, or
   opt-in config, plus whether the `.cmd`-shim quoting/escaping through `cmd.exe /c` is robust
   enough for arbitrary prompt content (only tested with trivial args so far).

`github.com/ubuntu/gowsl` cloned to `Reference-Repos/GoWSL` for its docs/examples; its
`examples/demo.go` confirms `Distro.Register(rootFsPath string)` takes exactly the kind of tarball
`build-rootfs.sh` produces.
