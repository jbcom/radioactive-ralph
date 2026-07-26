---
title: Deterministic execution
description: Normative CAS, isolated-attempt, artifact-publication, and completion contract for ralph.plan/v3.
---

# Deterministic execution

Status: **blocking design contract**. `ralph.plan/v2` is an experimental
explicit-DAG foundation. Trusted execution is additive `ralph.plan/v3`; Quest
must not activate against direct shared-checkout execution.

V3 keeps the `ralph-task` fence and requires:

```json
"contractVersion": "ralph.plan/v3"
```

An older binary rejects the unknown field instead of accidentally dispatching
the plan with legacy or v2 semantics.

## Purpose

The contract turns authored design into reproducible work without asking a
worker to rediscover inputs, outputs, authority, or completion conditions. A
worker receives the complete task envelope, runs in one isolated attempt
workspace, and can publish only declared content-addressed artifacts. A
provider exiting or emitting a plausible answer is never completion.

A durable task is not a worker. A plan may contain any number of tasks; only a
claimed attempt with an admission reservation consumes simultaneous capacity.

## Task contract

A v3 task contains:

- full identity, prose, team, `workClass`, dependencies, binding, capabilities,
  and `differentFrom`;
- the expected frozen project-source snapshot;
- named inputs and mount paths;
- named outputs with path, kind, and exclusive mode;
- structured acceptance argv, cwd, timeout, environment allowlist, and
  required output names;
- workspace, publication, and result-schema policy.

Arbitrary shell-string acceptance is not part of v3.

### Input sources

Every named input has exactly one source:

- `snapshot-path`: a file or tree in the frozen project snapshot with an
  expected manifest;
- `task-output`: symbolic `{task, output}` from an ordered ancestor;
- `cas`: an existing immutable manifest ID.

Example:

```json
{
  "name": "story-contract",
  "mount": "artifacts/story.json",
  "source": {
    "kind": "task-output",
    "task": "story.draft",
    "output": "story"
  }
}
```

The producer digest is intentionally unknown at import. Import proves the
producer and named output exist and a dependency path orders them. Claim
resolves the symbol to the producer's canonical publication and pins that
manifest to the attempt. A re-executed producer creates a new plan generation
and invalidates descendants; it never retargets an old publication.

### Outputs

Every named output declares:

- a portable relative path;
- kind `file`, `tree`, or `tombstone`;
- exclusive mode.

Portable paths reject traversal, special files, unsafe symlinks, case-folded
collisions, and Unicode-normalization collisions. Tree manifests are
canonically sorted and record path, type, mode, size, file SHA-256, and
explicitly permitted safe relative symlink text.

## Attempt lifecycle

### 1. Lock ingress

`plan lock` creates a frozen source manifest before headless import. It records
Git HEAD/tree and remote provenance plus exact tracked, modified, deleted, and
non-ignored untracked overlay. Regular files are SHA-256-addressed. The
operator checkout is not stashed, checked out, committed, or modified.

### 2. Stage and import

Content is staged into the XDG content-addressed store first. Graph, execution
contracts, snapshot, static bindings, and reservations are then inserted in
one database transaction. Failed import may leave only unreferenced CAS
objects, which garbage collection can safely remove.

### 3. Atomic claim

One immediate SQLite transaction:

1. verifies exact task and dependency readiness;
2. resolves every symbolic task output;
3. proves every CAS object exists;
4. evaluates file and adaptive-admission reservations;
5. increments `claim_epoch`;
6. creates the attempt and immutable input bindings;
7. records owner session/worker and exact provider provenance;
8. marks the task running.

No mutable project filesystem is inspected between admission and claim.

### 4. Isolated workspace

`internal/workspace` creates one XDG-owned directory per attempt. Git projects
use a Ralph-owned bare mirror sourced locally, never a worktree attached to the
operator's `.git` directory. The manager materializes the frozen dirty overlay
and resolved artifacts. Non-Git projects materialize from CAS.

The provider receives only the attempt directory as `WorkingDir`. The original
checkout is never a provider cwd or task-publication destination.

### 5. Complete prompt

The canonical execution envelope includes:

- plan, task, attempt, owner, and claim-epoch IDs;
- full prose and strict metadata;
- provider, invocation, calibration, and execution identity;
- source manifest;
- every input origin, mount, and manifest;
- every permitted output path and kind;
- structured acceptance and result schema;
- the rule that undeclared writes fail verification and are discarded.

Fenced metadata must not disappear during markdown parsing.

### 6. Capture candidate

After the worker exits, Ralph freezes the workspace and compares pre/post
manifests. It rejects:

- changed protected inputs;
- changes outside declared outputs;
- special files;
- unsafe paths or symlinks;
- output-kind mismatch;
- portable-path collisions.

Provider output is evidence to capture; it is not a completion decision.

### 7. Isolated acceptance

Acceptance runs in a fresh verification workspace assembled from the source
snapshot, attempt-pinned inputs, and captured candidate outputs. It uses
structured argv, a sanitized environment, and a timeout. Candidate output
manifests must remain unchanged across acceptance. Scratch changes are
destroyed.

### 8. Atomic publication

CAS writes use temporary files, content verification, `fsync`, and
same-filesystem rename. Existing digest paths make retries idempotent.

After CAS durability, one owner- and claim-epoch-guarded transaction:

- inserts canonical named publications;
- links them to the attempt and manifests;
- stores verification evidence;
- marks the attempt successful and task done;
- converges an active plan without overwriting `paused`, `abandoned`, or
  `archived`.

Downstream tasks read database/CAS publications, never mutable project paths.

### 9. Explicit export

Normal completion never writes into the dirty operator checkout. Results are
assembled into an XDG integration tree or private Git ref. An approved export
may update the operator checkout only after comparing every destination with
its import-time manifest. Drift fails closed. The default handoff is a patch,
bundle, or private integration ref.

## Recovery

Attempts persist:

```text
preparing -> running -> captured -> verifying -> publishing -> succeeded
                                     \-> failed / abandoned
```

- orphaned `preparing`: discard workspace and requeue;
- expired `running`: abandon attempt and requeue;
- `captured`: reuse candidate without another provider call;
- `verifying`: recreate verification workspace and rerun acceptance;
- `publishing`: inspect the journal and idempotently finish CAS/DB publication;
- unreferenced CAS objects remain safe garbage.

Every provenance, provider-session, capture, verification, and publication
write matches session, worker, attempt, and claim epoch. A stale worker cannot
alter a reassigned attempt.

Terminal task failure converges an active plan to `failed_partial`.
Successful convergence preserves operator-owned pause and abandonment.

## Store projection

The additive migration needs:

- source snapshots and root manifests;
- versioned execution contracts and hashes;
- input specs and immutable attempt bindings;
- named output specs and artifact manifests;
- task attempts and `tasks.claim_epoch`;
- canonical artifact publications;
- a publication journal.

Migration discovery and application share one immediate transaction.
Concurrent openers either apply a version once or observe it already applied.

## Implementation DAG

1. **Foundation hardening**
   - serialize migration;
   - owner-guard all provenance;
   - correct plan convergence;
   - use command-level IPC minimum versions.
2. **Deterministic ingress**
   - v3 parser and validator;
   - source lock, CAS, manifests, symbolic outputs.
3. **Deterministic execution**
   - artifact-aware atomic claims and attempt records;
   - private workspaces and complete prompts.
4. **Deterministic egress**
   - output-delta enforcement;
   - fresh acceptance workspace;
   - atomic resumable publication and recovery.
5. **Operational proof**
   - race, crash, path, dirty-checkout, UI, provider-identity, and adaptive
     admission integration.

## Required proof

- Old binaries reject v3 and mixed contract versions fail atomically.
- Static file/tree locks and hashes are reproducible.
- Symbolic producer output resolves to one immutable attempt binding.
- Missing, unordered, ambiguous, or unpublished producer references fail.
- Path-component and symlink swaps fail during snapshot, materialization,
  capture, acceptance, and publication.
- Case, Unicode, traversal, special-file, and output-tree collisions fail.
- Dirty tracked, untracked, and deleted checkout state remains byte- and
  status-identical.
- Writes outside outputs fail and never publish.
- Acceptance runs only in a fresh verification workspace.
- High-contention claims preserve reservation and single-owner invariants.
- Stale owner/claim-epoch writes cannot alter a reassigned attempt.
- Crash injection after every CAS, journal, verification, and publication
  boundary recovers idempotently.
- Restart resumes captured/publishing work without another provider call.
- Concurrent migration openers converge.
- Terminal failure, pause, abandonment, and rolling IPC behavior are correct.
- End to end, a producer publishes a tree, a consumer resolves it symbolically,
  and the original checkout remains untouched.

Quest activation remains blocked until implementation DAG stages 1–4 and the
core stage-5 proofs pass in a released build.
