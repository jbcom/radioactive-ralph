# Native Windows SCM safety disable

Status: accepted release-blocking safety contract for v0.22, 2026-07-26.

This specification supersedes the native Windows Service Control Manager
parts of the
[supervisor architecture](./2026-07-16-supervisor-architecture-design.md)
without rewriting that historical design. macOS launchd, Linux
`systemd --user`, WSL2 systemd, and native Windows foreground supervisor
control-plane execution are unchanged. Native Windows provider workers are
already unsupported because `creack/pty` returns `ErrPTYUnsupported`; this
specification does not claim otherwise.

## Decision

v0.22 does not install or start `radioactive_ralph --supervisor` as a native
Windows SCM service. Native Windows `service install` and any command path
that would start an existing Ralph SCM service must fail closed before
creating or changing a service, writing service configuration, or starting a
process.

Read-only status and uninstall remain available so an operator can identify
and remove an SCM registration created by an earlier development build. The
supported native Windows path is:

```powershell
radioactive_ralph --supervisor
```

It runs in the foreground as the interactive user and uses that user's state,
credentials, repositories, and SID-bound named pipe. WSL2 remains the
functional Windows route through the Linux pty and `systemd --user` backend.
Native foreground mode is limited to the supervisor/client control plane:
provider-backed worker turns remain unavailable.

This is a reductive release decision. It does not replace the unsafe service
with a scheduled task, a startup-folder entry, a system-wide service, or
another persistence mechanism in v0.22.

## Why the current SCM shape is unsafe

The current implementation cannot satisfy Ralph's one-user authority model:

1. A newly registered service defaults to `LocalSystem`, while Ralph's SQLite
   state, provider credentials, configuration, and repositories belong to the
   interactive user. The resulting process is not the same Ralph identity
   that the client initialized and authenticated.
2. An elevated `LocalSystem` process loading a binary, service configuration,
   environment, or state through paths writable by an unprivileged user turns
   replacement or configuration injection into a local privilege-escalation
   boundary.
3. To let the interactive client reach a `LocalSystem` service, the current
   named-pipe DACL grants read and write access to the broad interactive-user
   SID. Ralph's control API is mutating, so "any interactive user" is not an
   acceptable authorization policy.
4. Removing the installer's inherited `PATH` prevents one injection path but
   does not repair the service identity, filesystem ownership, credential
   access, repository access, or pipe authorization model.

Running the supervisor in the foreground avoids the SCM identity split: the
process, state root, and pipe owner resolve under the same interactive account.
It does not add native Windows pty support and therefore does not make the game
or other provider-backed work functional outside WSL2.

## v0.22 behavioral contract

- Native Windows SCM installation and service start are unsupported and fail
  before mutation. Elevation must not turn the operation into a supported
  path.
- The error identifies the alternatives accurately: foreground
  `radioactive_ralph --supervisor` for the limited native control plane, or
  WSL2 plus `systemd --user` for functional provider-backed execution.
- `service status` may inspect a prior Ralph SCM registration without starting
  it, but reports it as Ralph only after the historical executable, marker
  arguments, service metadata, and exact `UnitPath` under the resolved user
  home match. A same-name collision fails closed and is reported only as
  occupied SCM namespace, not as a valid installed Ralph service.
- `service uninstall` may stop and remove only a registration that matches
  that historical ownership contract. It re-verifies the live SCM definition
  immediately before deletion and must not delete the user's database,
  repositories, provider credentials, unrelated files, or an unknown
  same-name service.
- An SCM-hosted legacy process is rejected before Cobra/configuration with
  dedicated exit code 78, distinct from ordinary CLI failure code 1.
- Native Windows foreground supervisor and client discovery over the
  SID-bound named pipe remain supported as a limited control-plane path.
  Worker dispatch continues to fail with `ErrPTYUnsupported`.
- Cross-compilation and ordinary native Windows unit tests remain supported.
  They are not evidence that SCM installation is safe.
- Documentation, onboarding, and diagnostics must not tell native Windows
  users to elevate and retry `service install`.

## Re-enable gate

Native Windows service support may return only through a new, additive design
that proves all of the following. Passing only some criteria is not a basis
for experimental enablement in a stable release.

### Identity-bound service

- Installation names an explicit Windows account SID and never falls back to
  `LocalSystem`, `LocalService`, `NetworkService`, an arbitrary administrator,
  or a broad group.
- The service process, foreground client, user-level database, provider
  credentials, and repository access all resolve to that same intended user
  identity.
- Installation elevation is registration authority only. It does not become
  the runtime identity or leak the elevated installer's environment.
- Account changes and deleted/disabled accounts fail closed without silently
  changing identity.

### Filesystem and configuration ACLs

- The exact executable, service configuration, state database, log directory,
  and every ancestor used by the service have no reparse-point or
  lower-trust replacement path.
- Their Windows ACLs authorize only the bound user SID plus the minimum
  required `SYSTEM`/Administrators maintenance access. `Everyone`,
  `Authenticated Users`, `BUILTIN\Users`, unrelated interactive users, and
  other unprivileged SIDs cannot write or replace service-consumed bytes.
- Configuration and environment updates are validated and written atomically;
  a failed install or reconcile leaves the previously valid definition and
  files unchanged.
- Status reports the actual service SID and ACL validation result so identity
  drift is observable.

### Authorized control pipe

- The named-pipe DACL grants control access to the one bound user SID, with
  only the narrowly justified operating-system principals required for
  service operation.
- `WinInteractiveSid` and other broad interactive or authenticated-user grants
  are absent.
- A native integration test proves the authorized SID can use every intended
  client operation and a distinct local user SID cannot connect or mutate
  state.

### Provider and repository behavior

- Under the real service token, each shipped provider can be discovered and
  can load only that user's supported credentials without an interactive
  prompt.
- A real provider worker starts through a native Windows pty implementation;
  `ErrPTYUnsupported`, a POSIX/WSL subprocess, a mock provider, or a
  control-plane-only foreground run cannot satisfy this gate.
- The service identity can open the user's Ralph database and the intended
  repositories, including their Git metadata and worktrees, without granting
  access to unrelated users.
- Provider execution proves the service receives the intended `PATH`, home,
  profile, temporary-directory, credential-store, and repository context
  without inheriting administrator-only state.

### Native lifecycle proof

- A clean, supported native Windows host proves install, start, endpoint
  readiness, status, client connection, one real provider-backed repository
  turn, stop, restart, and uninstall under the identity-bound model.
- Install/reconcile and uninstall are failure-atomic and idempotent; a failure
  does not leave a partially registered or running service.
- Reboot/login behavior is exercised, and post-uninstall proof shows no Ralph
  service, service process, or service-owned pipe remains while user data is
  preserved.
- The clean native end-to-end runs in the required release gate. Mocked SCM
  calls, config serialization, cross-compilation, or a GitHub-hosted unit-test
  pass cannot substitute for it.

Until every gate is implemented and independently reviewed, native Windows SCM
support remains disabled.
