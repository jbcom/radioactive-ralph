---
title: Provider write containment
---

# Provider write containment

Ralph runs provider CLIs it does not control, against a checkout it does. This
document states what stops a provider writing outside that checkout, and —
just as importantly — where that guarantee does not yet hold.

## Validation is not containment

Two layers exist and they are not substitutes.

`internal/orch`'s `secureProjectPath` **validates**. It decides which declared
paths Ralph will read and which declared outputs Ralph will admit, and
completion re-checks the declarations. Every one of those operations happens
inside Ralph's own process.

None of it constrains the provider. The provider is a separate process that
opens files by pathname, minutes later, and **no string Ralph returns ever
travels into its syscalls**. Validation can say "this task declared an escaping
path". Only the kernel can say "this process may not write there".

The layers are complementary: validation catches the honest mistake early with
a good error, and containment is what makes the guarantee true when a provider
does something its declarations did not describe.

## The boundary

`internal/contain` produces a policy from an absolute, symlink-resolved root and
wraps the command so the kernel enforces it. The root is resolved because a
policy written against a symlink names the link rather than its target, so a
provider writing through the resolved path would land outside a boundary that
appears to contain it.

Containment is **opt-in per `agent.Start`** via `ContainmentRoot`, not derived
from `Dir`. Where a process starts is not the same claim as the only place it
may write, and quietly turning one into the other would change every existing
caller's behavior.

It **fails closed**. An unsupported platform or a relative root is an error from
`Start`, never a silently uncontained process — a caller that believes it is
contained when it is not is making exactly the false guarantee this replaces.

## Writes only, and why the profile allows by default

The macOS profile allows by default, denies all writes, then re-allows them
beneath the root.

Default-deny would be stronger in the abstract and worse in practice: it would
have to enumerate every read, mach lookup, and network call each provider CLI
needs — an open-ended list that differs per provider and per version, whose
first omission surfaces as a provider bug rather than a policy one. The
guarantee here is scoped to **writes**, so the profile denies exactly that.
Reads are NOT contained. The validation layer checks which paths *Ralph* will
read on a task's behalf; it places no restriction on the provider process, which
can read anything the user running Ralph can read. That is stated plainly here
because an earlier draft of this document implied validation covered provider
reads, which would have been a guarantee nobody was making. Network egress is
likewise out of scope and named as such below.

### There is no temp-directory exception, deliberately

An earlier draft granted the resolved `TMPDIR` so provider scratch files would
work. That single convenience line silently re-opened the boundary: on macOS
`TMPDIR` resolves under `/private/tmp`, so "allow writes to the temp dir"
allowed writes to a subtree holding other tools' and other users' files — while
the policy still reported containment.

The behavioral test caught it: an escape target in that subtree wrote
successfully. **A grant that widens the boundary is worse than no containment,
because it reports success.** A regression test now asserts every writable
subpath in the profile resolves to the containment root.

A provider needing scratch space writes it under the project root, which is
where a task's work belongs anyway. Verified: a compiled Go binary runs and
writes inside the root under this profile.

## Containment is inherited

macOS Seatbelt applies the policy to the process **and everything it spawns**.
That is load-bearing rather than incidental: a fan-out provider runs its own
sub-agents, so a boundary holding only for the top-level process would be
escaped by exactly the providers Ralph is built to run. There is a test for the
grandchild case specifically.

## Platform matrix

| Platform | Primitive | Status |
|---|---|---|
| macOS | `sandbox-exec` (Seatbelt) | **Enforced**, with behavioral tests proving an outside write is refused by the kernel, including from a grandchild process. |
| Linux | Landlock (5.13+) via a re-exec helper | **Enforced**, with behavioral tests proving an outside write is refused, an inside write lands, and a grandchild stays contained. |
| Windows (native) | — | **Not needed.** No provider can run there: `agent.Start` allocates a pty via `creack/pty`, which returns `ErrPTYUnsupported`, and the [Windows SCM safety spec](../superpowers/specs/2026-07-26-windows-scm-safety-disable-design.md) states it directly — *"Native Windows provider workers are already unsupported"*. Windows operators run Ralph under WSL, which is Linux. |

Unimplemented platforms return `ErrContainmentUnavailable` rather than passing
the command through. An untested containment claim is worse than an honest
refusal, because callers rely on it. Each platform lands with its own proof that
an outside write is actually refused, the way macOS's did, or it does not land.

Linux and macOS are both enforced. Native Windows is a **decision, not a gap**. Containment there would guard a
code path that cannot execute, and this repo treats dead code as a defect. A
test records the reasoning and fails if `Available()` ever reports true on
Windows — which would mean a provider path was added and the matrix needs
revisiting.

### Linux: why a re-exec helper

Landlock is applied by a process to **itself**, which forces the shape.

Below ABI 8 there is no `LANDLOCK_RESTRICT_SELF_TSYNC`, so
`landlock_restrict_self` binds only the *calling thread*. Measured on ABI 6: a
thread created **before** the call writes outside the root successfully. Go's
runtime already has threads running before any user code, so restricting
in-process from Go would ship a boundary with a hole — the same "reports success
while not containing" failure as the temp-directory grant above.

`execve` closes it. It replaces the process image with a single thread, and the
Landlock domain **is** inherited across it. So `Wrap` re-invokes Ralph's own
binary with a sentinel flag; that helper restricts itself and immediately execs
the provider. Nothing survives but the restriction. `main()` handles the
sentinel first, before flags or config — work done before the exec is either
outside the restriction or discarded.

### The handled-rights mask is the subtle part

Landlock denies a **handled** right everywhere no rule grants it. So the set of
handled rights must be exactly the mutating ones:

```
WRITE_FILE REMOVE_DIR REMOVE_FILE MAKE_* REFER TRUNCATE   // handled
EXECUTE READ_FILE READ_DIR IOCTL_DEV                       // deliberately NOT
```

An earlier draft used a mask written as "all write bits", `0x7fe`. Bit 2 is
`READ_FILE` and bit 3 is `READ_DIR`, so that mask handled two *read* rights and
granted them only under the root — making every file outside it unreadable,
including the provider binary and the dynamic loader. `execve` then failed with
`EACCES`, a symptom that points at exec permissions and says nothing about
reads.

Three tests guard the mask directly rather than only end-to-end: it must handle
no read/exec right, must stay within the ABI's defined bits (an undefined bit
makes `create_ruleset` return `EINVAL`), and must cover every mutating right —
a write right left *out* is a hole, since Landlock enforces only what it
handles.

## Out of scope

- **Network egress.** A contained provider can still reach the network. Scoping
  that needs a different mechanism and its own threat model.
- **Reads.** The provider can read whatever the user running Ralph can read.
  Nothing in either layer constrains that: containment here is about what a
  provider can *change*. Scoping reads would need the handled-rights set to
  include READ_FILE/READ_DIR, which breaks execve unless every path holding a
  binary or shared library is enumerated — a different design with a different
  cost.
- **Deliberate privilege escalation.** This raises the cost of an accidental or
  careless write outside the checkout. It is not a defense against a provider
  actively attacking the host.
