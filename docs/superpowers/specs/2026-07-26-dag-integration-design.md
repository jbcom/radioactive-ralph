# DAG integration design — folding `archive/plan-v2-dag` into main's plan/orchestrator

**Repo**: `/Users/jbogaty/src/jbcom/radioactive-ralph` (main = v0.22.1)
**Source**: tag `archive/plan-v2-dag` (11 commits, ex-PR #198, closed superseded)
**Worktree**: `/Users/jbogaty/src/jbcom/.worktrees/rr-dag-reland` on `feat/plan-v2-reland`
**Merge-base**: `c6eb5d1` — the branch predates `eb04193` (#202) and `99b4b06` (#203).

---

## 0. The central finding: main ALREADY has the DAG store layer

The source branch's framing ("v2 adds a DAG") is wrong about the store. `0001_initial.up.sql`
already ships the edge table, and the comment says so explicitly:

```sql
-- task_deps: the DAG edges. Cycle prevention lives in Go (AddDep).
CREATE TABLE task_deps (
  plan_id    TEXT NOT NULL,
  task_id    TEXT NOT NULL,
  depends_on TEXT NOT NULL,
  PRIMARY KEY (plan_id, task_id, depends_on),
  FOREIGN KEY (plan_id, task_id)    REFERENCES tasks(plan_id, id) ON DELETE CASCADE,
  FOREIGN KEY (plan_id, depends_on) REFERENCES tasks(plan_id, id) ON DELETE CASCADE,
  CHECK (task_id != depends_on)
);
CREATE INDEX task_deps_dependent ON task_deps(plan_id, depends_on);
```

`internal/store/tasks.go:19` even documents `Task` as *"a DAG node"*. And `Ready` /
`ClaimNextReady` already walk the edges — this is main's `Ready`, unchanged:

```go
FROM tasks
WHERE plan_id = ?
  AND status IN ('pending', 'ready')
  AND NOT EXISTS (
    SELECT 1 FROM task_deps d
     JOIN tasks tdep ON tdep.plan_id = d.plan_id AND tdep.id = d.depends_on
    WHERE d.plan_id = tasks.plan_id
      AND d.task_id = tasks.id
      AND tdep.status NOT IN ('done', 'skipped', 'decomposed')
  )
ORDER BY created_at
```

`AddDep` (tasks.go:137) + `wouldCreateCycle` (tasks.go:160) already reject cycles by DFS.

**The actual gap is in the orchestrator, not the store.** `DispatchNext`
(orchestrator.go:417) never consults `task_deps`. It re-parses the markdown every pass and
derives readiness *positionally*:

```go
parsedPlan, err := plan.Parse([]byte(storedPlan.SourceMarkdown))
done, err := o.doneSet(ctx, planID)
readySteps, refs, parallel := plan.DecomposeRefs(parsedPlan, done)
```

`plan.Decompose` computes readiness from document order (decompose.go): *"document order
encodes dependency: an earlier group must complete before a later one starts"*. Task IDs
are positional strings — `StepRef.ID()` returns `"0.1.2"`. No `AddDep` call exists anywhere
in `internal/orch`.

So the integration is **not** "add a DAG". It is: **make the plan grammar able to express
explicit edges, persist them into the `task_deps` table that already exists, and switch
dispatch from positional decomposition to the graph-walking `Ready`/`ClaimNextReady` the
store already implements.** A linear plan then produces a chain of edges — the degenerate
DAG — through one code path.

This reframing kills most of the source branch's surface area: `store/plan_graph.go` (417
lines) is largely a reimplementation of `CreateTask`+`AddDep` inside one transaction, and
`orch/v2_dispatch.go` is a second dispatch loop that could have been the first one.

---

## A. Mapping table

### A.1 `internal/plan`

| Source file / feature | Lands in | Discard |
|---|---|---|
| `v2.go` → `TaskMetadata{ID,After,Team,Binding,Requires,Providers,DifferentFrom,Inputs,Outputs}` | **`internal/plan/types.go` (new, non-versioned)** — main has no `types.go`; `Plan`/`Group`/`Step` are declared inline at the top of `parse.go`. Move them out to `types.go` and add `Step.Metadata *TaskMetadata` there. This is the one genuinely-new *file*, justified because it holds the type surface, not a version. | The `Plan.V2 bool` discriminator field. A bool that switches between two execution contracts is exactly the parallel-world smell. Readiness is a property of edges, not of a document flag. |
| `v2.go` → `parseTaskMetadataBlock` (goldmark `FencedCodeBlock` walk for ` ```ralph-task `) | **`internal/plan/parse.go`**, called from the existing `parseLeafSteps` loop, right where `parseStepMarkers` is already called. The `[approval]` marker precedent is exact: main already extracts structured meaning out of a list item there. | — |
| `v2.go` → `requireMetadataFields`, `ensureJSONEOF`, `validateTaskMetadata`, `validateTaskBinding`, `validateTaskBindingSyntax`, `validateTaskLists`, `validateTaskPaths`, `validateRelativePath`, `uniqueTaskPaths`, `inputPaths`/`outputPaths`, `unique`, `contains` | **`internal/plan/validate.go`** — extends the existing file. `validateRelativePath` is load-bearing (see §E.4). | — |
| `v2.go` → `V2Task` struct + `(*Plan).V2Tasks()` + `countMetadata` | **`internal/plan/decompose.go`** — main already has `walkGroups(groups, path, fn)` doing the identical document-order flatten. `V2Tasks` is `walkGroups` with a different accumulator. Fold into a `func (p *Plan) Tasks() []TaskRef` built on `walkGroups`. Do **not** add a second walker. | The `V2Task.Order int` field — `walkGroups` already yields document order; the index is the enumeration position. |
| `v2_validate.go` → `ValidateV2` | **`internal/plan/validate.go`**, merged into `ValidateForImport`. Main's `ValidateForImport` is already the ingress fail-closed gate ("*ingress must fail closed instead of persisting a document whose dispatch order the heuristic grammar cannot determine*") — dependency validation is the same contract, one more clause. | The "metadata is mixed with legacy steps" all-or-nothing rule. Partial annotation must be legal — but see the edge-derivation table below for exactly what "partial" means, because "no metadata" and "metadata with no `after`" are NOT the same case. |
| `v2_validate.go` → `dependencyCycle` (DFS colouring, returns the cycle path) | **`internal/plan/validate.go`** as `dependencyCycle`. Worth keeping over the store's `wouldCreateCycle` at the *document* layer because it reports the cycle path for the error message; the store's incremental check stays as the persistence-layer backstop. | — |
| `v2_validate.go` → `transitivelyDepends`, `validateOutputOverlaps`, `pathsOverlap`, `validateInputOutputOverlaps` | **`internal/plan/validate.go`**. | — |
| `v2_test.go`, plus new dependency cases | **`internal/plan/validate_test.go`** and **`internal/plan/plan_test.go`** — append to existing files, matching main's layout (`plan_test.go` = parse, `validate_test.go` = validate, `decompose_test.go` = decompose). | `internal/plan/v2_test.go` as a filename. |
| `parse.go` diff (−75/+... on the branch) | Re-derive from main's current `parse.go`, do not port the branch's version — the branch rewrote the doc comment and moved the types out. Only the `parseTaskMetadataBlock` call site is needed. | The branch's wholesale `parse.go` replacement. |

#### Edge derivation — the three cases are distinct

An earlier draft said both "no `after:` means no incoming edges" and "absent metadata
falls back to document order." Those disagree, and the same plan could import as two
different graphs depending on which sentence a reader followed. The rule is:

| Case | Meaning | Edges |
|---|---|---|
| **No ` ```ralph-task ` block at all** | An unannotated step. This is every plan that exists today. | **Document order.** Sequential leaf → chain each step to its predecessor; parallel leaf → chain the whole group to the previous group's terminals. This is what makes a linear plan the degenerate DAG and what keeps existing plans importing identically. |
| **Block present, `after` key omitted** | The author annotated the step (binding, team, inputs/outputs) but said nothing about ordering. | **Document order**, exactly as above. Annotating a step must not silently change its position in the graph — that would make adding a `team:` field reorder execution. |
| **Block present, `after: []`** | The author explicitly declared no dependencies. | **No incoming edges.** A root node, ready immediately. This is the only way to opt a step out of document order. |
| **Block present, `after: [ids…]`** | Explicit dependencies. | **Exactly those edges.** Every id must resolve to a task in the same plan; an unknown id fails `ValidateForImport`. Document order is NOT additionally applied — the author has taken ownership of this node's ordering. |

Empty-vs-omitted is therefore load-bearing and the parser must distinguish them: `After`
is `*[]string`, not `[]string`. `nil` means omitted, non-nil-empty means explicitly none.

Regression test (`internal/plan/validate_test.go`,
`TestImportPlanMixedAnnotationEdgeDerivation`): one plan containing all four cases,
asserting the exact edge set. Without it these four rules are prose, and the first
refactor collapses `nil` and `[]` back together.

### A.2 `internal/orch`

| Source file / feature | Lands in | Discard |
|---|---|---|
| `plan_import.go` → `ImportPlan` / `ImportPlanOpts` / `ErrInvalidPlanContract` | **`internal/orch/plan_import.go` (new, non-versioned)** — justified: main has *no* plan-ingress function today. `cmd/radioactive_ralph/plan_cmd.go` and the IPC path each call `store.CreatePlan` directly. A single validated ingress that materializes tasks + edges is a genuinely new concept. | The `if !parsed.V2 { return o.importLegacyPlan(...) }` fork and `importLegacyPlan` itself. One import path: validate → parse → **one `store.CreatePlanGraph` call** carrying every node and edge in a single transaction (see the transactional-import correction below §A.3 — sequential `CreateTask`/`AddDep` calls each autocommit and cannot be atomic). A plan with no `after:` clauses materializes a chain of edges from document order — same function, degenerate input. |
| `v2_dispatch.go` → `dispatchNextV2` | **`internal/orch/orchestrator.go`, inside the existing `DispatchNext`.** Replace the `plan.DecomposeRefs(parsedPlan, done)` readiness computation with `o.store.Ready(ctx, planID)`. Everything downstream of readiness in `DispatchNext` — the `maxParallel` admission budget, `stepGateBlocks`, `acquireDispatchSlot`, `dispatchReadyStep`, the native-fanout branch — is already graph-agnostic and stays verbatim. | The whole second dispatch loop. Also discard `dispatchNextV2`'s `byID` map built from `parsed.V2Tasks()` on every pass: once edges are persisted at import, dispatch does not need to re-parse markdown to know readiness. |
| `v2_dispatch.go` → `separatedDomains`, `resolveV2DispatchBinding`, `v2DispatchBinding` | **`internal/orch/calibration_admission.go` (new, non-versioned)** — team-lead brief already blesses calibration as a new concept. These are calibration-lane resolution, not dispatch. | — |
| `v2_admission.go` → `secureProjectPath`, `resolveThroughExistingAncestor`, `pathContained`, `pathWithinDeclaredOutput`, `validateV2Filesystem`, `validateV2Inputs`, `verifyV2CompletionFilesystem` | **`internal/orch/task_paths.go` (new, non-versioned)** — path containment for task-declared inputs/outputs is a new concept with no existing home. Rename the entry points: `validateTaskFilesystem`, `verifyTaskCompletionFilesystem`. **Must be fixed before landing — see §E.4.** | The `V2`/`v2` in every identifier. |
| `binding_constraints.go` → `BindingConstraints{AllowedProviders,Requirements}` | **`internal/orch/binding_constraints.go`** — already non-versioned, port as-is. | — |
| `calibration_admission.go` (255 lines) + `calibration_admission_test.go` | **`internal/orch/calibration_admission.go`** — port as-is; already correctly named. | — |
| `verify.go` → `Acceptance.RequiredOutputs`, `strictV2AcceptanceJSON`, `acceptCommandMarkerRe`/`acceptFileMarkerRe`, the `mechanicalAcceptanceCheck` `RequiredOutputs` loop | **`internal/orch/verify.go`** — extends existing. Rename `strictV2AcceptanceJSON` → `strictAcceptanceJSON`. | The `VerifyAndComplete` restructure that calls `verifyV2CompletionFilesystem` **twice** (before and after `acceptanceCheck`) — see §E.5. Keep the pre-check only. |
| `orchestrator.go` (+425 lines on the branch) | Re-derive against main. The branch's diff is against `c6eb5d1`; main's `DispatchNext` has since gained the native-fanout branch and the `stepGateBlocks` pre-check. **Never port this file's diff — hand-apply the readiness swap.** | The branch's `dispatchStepArgs` extension carrying `alias`/`model`/`effort`/`independenceDomain`/`calibrationMode`/`calibrationRepetitions`/`calibrationFixture` as seven loose fields. Carry one `*calibrationLane` pointer instead. |
| `plan_import_v2_test.go`, `v2_admission_test.go`, `v2_dispatch_test.go`, `v2_completion_boundary_test.go` | **`internal/orch/plan_import_test.go`**, **`internal/orch/task_paths_test.go`**, folded into **`internal/orch/orchestrator_test.go`** / **`dispatch_verify_test.go`**, **`internal/orch/completion_boundary_test.go`**. | Every `v2` filename and `TestV2*` test name. |

### A.3 `internal/store`

| Source file / feature | Lands in | Discard |
|---|---|---|
| `schema/0003_plan_v2.up.sql` → `task_metadata` table | **`schema/0003_plan_graph.up.sql`** (named for behavior). Keep — this is the durable execution-provenance record with no existing home. | — |
| `schema/0003_plan_v2.up.sql` → `task_input_reservations`, `task_output_reservations` | Same migration. Keep. | — |
| `schema/0003_plan_v2.up.sql` → `provider_calibrations`, `task_calibration_attempts` | Same migration (calibration is a blessed new concept). | — |
| `plan_graph.go` → `CreatePlanGraph`, `CreatePlanGraphOpts`, `GraphTaskSpec`, `insertGraphPlan`, `insertGraphTask` | **Discard the atomic-create wrapper.** It re-implements `CreatePlan` + `CreateTask` + `AddDep` with raw SQL inside one tx, duplicating three existing methods and *bypassing `AddDep`'s cycle check* (it INSERTs into `task_deps` directly). **CORRECTED (see the note below §A.3): sequential `CreatePlan`/`CreateTask`/`AddDep` calls CANNOT be atomic** — each autocommits. Add `*Tx` variants and one `CreatePlanGraph` that composes them in a single transaction. | `insertGraphPlan` duplicates `CreatePlan`'s INSERT verbatim including the `ErrDuplicateSlug` mapping. `insertGraphTask` duplicates `CreateTask`'s INSERT + `RequiresApproval` status logic. |
| `plan_graph.go` → `TaskExecutionMetadata`, `GetTaskExecutionMetadata`, `RecordTaskProvider`, `RecordTaskProviderSession`, `BindTaskCalibration`, `MarkBlockedCapability`, `MarkBlockedInput`, `markMetadataBlocked` | **`internal/store/task_metadata.go` (new, non-versioned)** — genuinely new: durable per-task execution provenance. | The name `plan_graph.go` for this file — it holds metadata, not the graph. The graph is `task_deps`, owned by `tasks.go`. |
| `graph_validate.go` → `validateGraphSpecs` | Discard — only exists to validate `CreatePlanGraphOpts`, which is itself discarded. Its checks (unknown dep target, self-dep) are already enforced by `AddDep` + the FK on `task_deps`. | Whole file. |
| `exact_claim.go` → `ClaimReadyTask` (claim one *named* dependency-ready task) | **`internal/store/tasks.go`**, beside `ClaimNextReady`. This is a real gap: `ClaimNextReady` picks whichever task the ORDER BY surfaces, and `orch.claimStepTask` has to paper over it — *"ClaimNextReady claimed a DIFFERENT ready task ... That's fine — it is still a valid step to dispatch, so resolve it back to its plan.Step via its ID"*. With explicit edges, dispatch wants a named claim. Name it `ClaimTask`. | `ErrOutputReserved` + the output-reservation predicate inside the claim tx, **for increment 1** — land it in the reservations increment so the claim change stays reviewable in isolation. |
| `exact_claim.go` → `exactClaimGate` mutex + `exactClaimBusyRetryWindow`/`Backoff` SQLITE_BUSY retry loop | **`internal/store/tasks.go`** with the claim. Rename `claimGate`. Sound: SQLite admits one writer, so in-process serialization of short claim txs avoids turning a ready-wave into `SQLITE_BUSY`. | The `v2`/`exact` naming. |
| `store.go` diff → `ensureJournalModeWAL` + moving `journal_mode(WAL)` out of the DSN | **`internal/store/store.go`**. Keep — the rationale is correct and independently valuable: *"Putting journal_mode(WAL) in the DSN runs the lock-taking pragma independently on every newly opened pooled connection."* **Land this as its own commit**; it is a real bug fix orthogonal to the DAG. | — |
| `tasks.go` diff → `TaskStatusBlockedCapability`, `TaskStatusBlockedInput` + `StatusCounts` `Blocked +=` fold | **`internal/store/tasks.go`**. Keep. | — |
| `tasks.go` diff → `Task` struct + 11 metadata fields, `enrichTaskMetadata` called from `GetTask`/`ListTasks` | **Discard the widening.** Making every `GetTask` fire a second LEFT JOIN query for fields 90% of callers ignore is a hot-path tax. Keep `Task` as the DAG node it is documented to be; callers that want provenance call `GetTaskExecutionMetadata` explicitly. `internal/store/task_metadata_view.go` (139 lines) exists only to serve this widening — discard it too. | `enrichTaskMetadata`, `task_metadata_view.go`. |
| `tasks.go` diff → `ErrTaskNotOwnedRunning` rewrapped as `fmt.Errorf("%w ...", ErrTaskNotRunning)` | Keep — a proper sentinel hierarchy. | — |
| `tasks.go` diff → `convergeV2PlanTx` calls in `MarkDone`/`MarkFailedWithPayload` | **`internal/store/tasks.go`** as `convergePlanTx`. Keep — plan-status convergence on terminal task transitions is correct and applies to every plan. | The `V2` in the name and any behavior gated on a v2 flag. |
| `task_recovery.go` (79 lines) | **`internal/store/task_recovery.go`** — port as-is, already non-versioned. | — |
| `calibrations.go`, `calibration_attempts.go` | Port as-is, already non-versioned. | — |
| `workers.go` diff → `RunningWorker` + `TeamPath`/`Alias`/`Model`/`Effort`/`IndependenceDomain`/`AssignedSessionID`/`ProviderSessionID`, LEFT JOIN on `task_metadata` | **`internal/store/workers.go`**. Keep — `ListRunningWorkers` is a low-frequency operator-facing query, so the JOIN cost is fine here (unlike `GetTask`). | — |
| `migrate.go` diff (+35) | **`internal/store/migrate.go`** — the only required change is `currentSchemaVersion = 2` → `3`. Inspect the branch's other +35 lines and take only what is needed. | Any `.down.sql` machinery if the branch added it — main is forward-only (`listMigrations(schema.FS, ".up.sql")`, no down path). |
| `plan_v2_lifecycle_test.go`, `plan_v2_scheduling_test.go`, `plan_graph_test.go` | Fold into **`internal/store/tasks_test.go`** and **`internal/store/task_metadata_test.go`**. | All three filenames. |

> **Correction — plan import must be transactional (Codex P2 on PR #210, accepted).**
>
> The row above originally said to replace `CreatePlanGraph` with sequential
> `store.CreatePlan` → `store.CreateTask` → `store.AddDep` calls. That is wrong.
> Verified against the tree: `CreatePlan` (`plans.go:68`), `CreateTask`
> (`tasks.go:93`), and `AddDep` (`tasks.go:137`) each issue a bare
> `s.db.ExecContext` and autocommit independently, and the package exposes **no**
> `*Tx` variants to compose (`config.go`, `projects.go`, `reaper.go`, and
> `tasks.go` all call `s.db.BeginTx` internally and keep the tx private).
>
> So a mid-import failure or context cancellation would leave a `draft` plan plus
> whatever nodes already committed, and the retry would then hit
> `ErrDuplicateSlug` instead of completing the graph — a plan permanently stuck
> undispatchable, which is exactly the fail-closed-ingress property
> `ValidateForImport` exists to guarantee.
>
> **Revised increment 5:** add unexported `createPlanTx`, `createTaskTx`, and
> `addDepTx` carrying `*sql.Tx`, refactor the three public methods to be
> one-statement wrappers over them (no duplicated SQL — same statements, one
> owner), and add a single public `CreatePlanGraph(ctx, opts)` that opens one
> transaction and calls them. Cycle checking stays in `addDepTx` so the graph
> path cannot bypass it — the specific flaw that made the source branch's version
> unsafe. `ImportPlan` calls `CreatePlanGraph` once. Increment 5's file list gains
> `internal/store/plans.go` and `internal/store/tasks.go`; its test list gains a
> case asserting that a failed import leaves **no** plan row behind.

### A.4 Clients + provider

| Source file / feature | Lands in | Discard |
|---|---|---|
| `provider/invocation.go` → `Invocation`, `ResolveInvocation` | **`internal/provider/invocation.go`** — port, but re-derive against main's post-#202 `claude.go`/`codex.go`/`opencode.go`. | — |
| `provider/capabilities.go` → `KnownCapability`, `CalibrationRequiredCapability`, `SupportsRequirements` | **`internal/provider/capabilities.go`** — port as-is. | — |
| `provider/provider.go` → `Binding.CalibratedCapabilities`, `Request.StrictBinding`, `Result.Invocation` | **`internal/provider/provider.go`** — port the three field additions. | — |
| `provider/codex.go`, `claude.go`, `opencode.go` diffs | **Hand-apply only the `ResolveInvocation` call + `Result.Invocation` plumbing** onto main's current files. | The branch's versions of these files wholesale — see §E.1/§E.2. |
| `provider/procgroup.go` (+23) | Check against main's post-#202 `killgroup_unix.go`/`killgroup_windows.go`; #202 rewrote process-group handling. Likely already superseded. | Probably all of it. |
| `tui/team.go`, `tui/team_view.go`, `tui/model.go`, `tui/meso.go`, `tui/micro.go` | **Port as-is** — team views are a blessed new capability, already non-versioned. | — |
| `gui/team.go`, `gui/views.go`, `gui/app.go`, `gui/live.go`, `gui/controller.go` | **Port as-is**, but re-derive `live.go`/`app.go` against main's post-`835fc2b` (#201) "keep lifecycle work on the live event loop" changes. | — |
| `ipc/protocol.go`, `server.go`, `client_drive.go`, `transport_windows.go` | Port the team/calibration RPC surface. | `008d752` "bound Windows named-pipe shutdown" — verify against the `2026-07-26-windows-scm-safety-disable-design.md` contract first; that spec supersedes the architecture spec's Windows clauses. |
| `cmd/radioactive_ralph/binding_resolver.go`, `provider_cmd.go`, `plan_cmd.go` | Port; `plan_cmd.go` switches to `orch.ImportPlan`. | — |
| `docs/design/plan-v2-adaptive-concurrency.md` | **`docs/design/plan-adaptive-concurrency.md`** — rename. | The `plan-v2` in the filename. |
| `docs/design/deterministic-execution.md`, `exact-provider-identity.md`, `guides/plan-format.md` | Port; scrub `v2` from prose where it names a version rather than a concept. | — |

---

## B. Type / schema design

### B.1 Plan types

Move the inline types out of `parse.go` into a new `internal/plan/types.go` and add one field:

```go
// Step is one list item plus its optional narrative detail and metadata.
type Step struct {
	Text             string
	Detail           string
	RequiresApproval bool

	// Metadata carries the step's explicit dependency and execution contract,
	// parsed from a fenced ```ralph-task JSON block inside the list item.
	// Nil means the step takes its edges from document order: the heading/list
	// grammar is the degenerate spelling of the same graph.
	Metadata *TaskMetadata
}
```

`Plan` and `Group` move unchanged. **No `Plan.V2` field.**

`TaskMetadata` ports from `v2.go` with version-free field names, but **`After` is
`*[]string`, not `[]string`** — see the edge-derivation table in §A.1. A plain slice
cannot distinguish an omitted `after` (document order applies) from an explicitly empty
one (an unconditioned root), and collapsing those two would let the same plan import as
two different graphs. `DependsOn() (ids, stated)` is the accessor; callers branch on
`stated` rather than inferring intent from emptiness.

### B.2 SQLite: edges table, not columns — and it already exists

**Edges belong in `task_deps`, which main already ships.** No new edge storage.

Rationale against columns on `tasks`: an `after_json TEXT` column would make `Ready`'s
`NOT EXISTS` subquery impossible without JSON1 unnesting on every dispatch pass, would
lose the `FOREIGN KEY (plan_id, depends_on)` referential guarantee that makes a dangling
edge impossible, would lose `CHECK (task_id != depends_on)`, and would lose the
`task_deps_dependent` index that makes reverse-edge lookup cheap. The existing table is
correct; the orchestrator simply never writes to it.

The only genuinely-new storage is execution provenance and path reservations.

### B.3 Migration: `0003_plan_graph.up.sql`

Named for behavior, not version. Contents (from the branch's `0003_plan_v2.up.sql`,
renamed, comment header rewritten):

- `task_metadata` — PK `(plan_id, task_id)`, FK → `tasks(plan_id, id)` ON DELETE CASCADE.
  Index `task_metadata_team ON task_metadata(plan_id, team_path)`.
- `task_metadata.group_path TEXT NOT NULL` — the task's leaf-group identity, as the
  dotted `StepRef.GroupPath` (e.g. `"0.2"`). **Load-bearing for increment 6**, not
  decoration: `dispatchFanoutGroup` delegates a whole partition to ONE provider under one
  group heading, so the partition key must be durable. Deriving it at dispatch time would
  mean re-parsing the markdown, which is exactly the positional dependence the graph walk
  removes. Populated by `ImportPlan` from the `StepRef` it already holds while walking
  groups; index `task_metadata_group ON task_metadata(plan_id, group_path)`.
  Accessor: `GroupPath` on `TaskExecutionMetadata`, plus a
  `ListTaskGroupPaths(ctx, planID) (map[string]string, error)` so dispatch can partition a
  ready wave in one query instead of N.
- `task_input_reservations` — PK `(plan_id, task_id, path)`, `sha256` pin. Index on
  `(plan_id, path)`.
- `task_output_reservations` — PK `(plan_id, task_id, path)`,
  `CHECK (mode = 'exclusive')`.
- `provider_calibrations` — `id` PK, `alias` UNIQUE.
- `task_calibration_attempts` — PK `(plan_id, task_id, attempt_sequence, repetition)`.

**Additive and forward-only — confirmed against `internal/store/migrate.go`:**

- Every statement is `CREATE TABLE` / `CREATE INDEX`. No `ALTER`, no `DROP`, no rewrite of
  `tasks`, `plans`, or `task_deps`. An existing v2 DB gains tables and loses nothing.
- Forward-only is structural: `Migrate` reads `listMigrations(schema.FS, ".up.sql")` — the
  `.up.sql` suffix is hard-coded and there is no down path anywhere in the package. Ship no
  `0003_plan_graph.down.sql`.
- Application is version-gated and transactional: `for _, m := range upFiles { if m.version <= dbVersion { continue } ... applyMigration(db, m.version, string(body)) }`, and
  `applyMigration` runs the body + `PRAGMA user_version = N` inside one `tx`, rolling back
  on any error (proven by `TestApplyMigrationExecFailureDoesNotBumpVersion`).
- **Required companion edit**: `const currentSchemaVersion = 2` → `3` in `migrate.go:14`.
  Without it, `Migrate` still applies 0003 (the loop is driven by `upFiles`, not the
  constant) but every subsequent `Open` fails the guard `dbVersion > currentSchemaVersion`
  → *"DB schema version 3 is newer than this binary supports (2)"*. `TestRefuseNewerSchema`
  covers exactly this.
- `TestForeignKeyCascade` and `TestOpenRunsMigrations` must keep passing unchanged.

### B.4 Status enum

Add to `internal/store/tasks.go`:

```go
TaskStatusBlockedCapability TaskStatus = "blocked_capability"
TaskStatusBlockedInput      TaskStatus = "blocked_input"
```

The `tasks.status` column is `TEXT NOT NULL DEFAULT 'pending'` with **no CHECK
constraint** — only a comment listing values. So new statuses need no migration. But both
must be added to `Ready`'s and `ClaimNextReady`'s exclusion sets (they are already excluded
by `status IN ('pending','ready')`) and to `doneSet`'s *non*-satisfying set (already, since
`doneSet` only matches done/skipped/decomposed). Fold both into `StatusCounts`'s `Blocked`
bucket, as the branch does.

---

## C. Ordered increments

Each compiles (`go build ./...`) and passes tests in isolation on top of its predecessors.
File counts stay well under CodeRabbit's 100-file skip threshold.

---

### Increment 1 — WAL pragma fix (independent, land first)

Orthogonal to the DAG; landing it first keeps it reviewable and de-risks the rest.

- **Files (3)**: `internal/store/store.go`, `internal/store/store_test.go`,
  `docs/api/internal/store.md`
- **Change**: move `journal_mode(WAL)` out of the DSN into a one-time `ensureJournalModeWAL`
  after `Ping`, with a bounded `SQLITE_BUSY` retry.
- **Test**: `go test ./internal/store/ -run 'TestOpen|TestDSNEscaping|TestConcurrentWriters|TestWriteSucceedsDuringBackup' -race`
- **Depends on**: nothing.

---

### Increment 2 — schema + task metadata store

- **Files (~8)**: `internal/store/schema/0003_plan_graph.up.sql` (new),
  `internal/store/migrate.go` (`currentSchemaVersion` 2→3),
  `internal/store/task_metadata.go` (new), `internal/store/task_metadata_test.go` (new),
  `internal/store/tasks.go` (two status constants + `StatusCounts` fold),
  `internal/store/tasks_test.go`, `internal/store/migrate_test.go`,
  `docs/api/internal/store.md`
- **Change**: land the migration; `GetTaskExecutionMetadata`, `RecordTaskProvider`,
  `RecordTaskProviderSession`, `MarkBlockedCapability`, `MarkBlockedInput`. No orchestrator
  changes; nothing writes these tables yet.
- **Must include `group_path`** (see §B.2) on the `task_metadata` table, on
  `TaskExecutionMetadata`, and as `ListTaskGroupPaths`. Increment 6's fan-out partitioning
  depends on it, so it has to exist in the schema from the start — adding it later would
  mean a second migration for a field the design already requires.
- **Test**: `go test ./internal/store/ -race` — `TestOpenRunsMigrations`,
  `TestRefuseNewerSchema`, `TestReopenIsIdempotent`, `TestForeignKeyCascade` must pass
  unchanged. New: `TestTaskMetadataRoundTripsGroupPath`.
- **Depends on**: 1.

---

### Increment 3 — named claim + plan convergence

- **Files (~5)**: `internal/store/tasks.go`, `internal/store/tasks_test.go`,
  `internal/store/task_recovery.go` (new), `internal/store/workers.go`,
  `internal/store/workers_test.go`
- **Change**: `ClaimTask(ctx, planID, taskID, sessionID, workerID)` — same readiness
  predicate as `ClaimNextReady` but bound to a named id, with the `claimGate` mutex +
  `SQLITE_BUSY` retry. `convergePlanTx` in `MarkDone`/`MarkFailedWithPayload`.
  `RunningWorker` metadata JOIN. `ClaimNextReady` untouched.
- **Test**: `go test ./internal/store/ -race` — `TestClaimNextReadyConcurrentUniqueness`,
  `TestClaimNextReadyHonorsSequenceOrdinal`, `TestApprovedReadyTaskIsClaimable`,
  `TestReclaimWorker` unchanged; new `TestClaimTaskRefusesUnreadyTask`,
  `TestClaimTaskConcurrentUniqueness`.
- **Depends on**: 2.

---

### Increment 4 — plan model learns dependency edges

- **Files (~7)**: `internal/plan/types.go` (new — types moved out of `parse.go`),
  `internal/plan/parse.go`, `internal/plan/validate.go`, `internal/plan/decompose.go`,
  `internal/plan/plan_test.go`, `internal/plan/validate_test.go`,
  `docs/api/internal/plan.md`
- **Change**: `TaskMetadata` + friends in `types.go`; `parseTaskMetadataBlock` called from
  `parse.go`'s leaf-step loop; metadata/dependency/cycle/overlap validation folded into
  `ValidateForImport`; `(*Plan).Tasks()` built on the existing `walkGroups`. Pure
  parse/validate — no store, no orchestrator.
- **Test**: `go test ./internal/plan/ -race` — every existing `TestParse*`,
  `TestValidate*`, `TestDecompose*` must pass **unchanged** (see §D).
- **Depends on**: nothing in the store; can land in parallel with 2/3, but sequence it here
  so increment 5 has both halves.

---

### Increment 5 — plan ingress materializes tasks + edges

The keystone. This is where a linear plan and a DAG plan become one path.

- **Files (~8)**: `internal/orch/plan_import.go` (new),
  `internal/orch/plan_import_test.go` (new), `internal/store/plans.go`,
  `internal/store/tasks.go`, `internal/orch/verify.go`
  (`strictAcceptanceJSON` + `Acceptance.RequiredOutputs`),
  `internal/orch/acceptance_derive_test.go`, `cmd/radioactive_ralph/plan_cmd.go`,
  `docs/api/internal/orch.md`
- **Store change (prerequisite, same increment)**: add unexported `createPlanTx`,
  `createTaskTx`, and `addDepTx` taking `*sql.Tx`; refactor the three public methods into
  one-statement wrappers over them so the SQL has exactly one owner; add one public
  `CreatePlanGraph(ctx, opts)` that opens a single transaction and composes them. Cycle
  checking runs inside `addDepTx`, through the same transaction, so the graph path cannot
  bypass it — that bypass is precisely what made the source branch's version unsafe.
- **Change**: `ImportPlan` — `plan.ValidateForImport` → `plan.Parse` → one
  `store.CreatePlanGraph` call carrying every node and edge. Edges come from
  `Metadata.After` when present; **otherwise from document order** (sequential leaf →
  chain each step to its predecessor; parallel leaf → chain the whole group to the
  previous group's terminals). One function, no fork, one transaction.
- **Why not sequential calls**: `CreatePlan` (`plans.go:68`), `CreateTask`
  (`tasks.go:93`), and `AddDep` (`tasks.go:137`) each issue a bare `s.db.ExecContext` and
  autocommit; no `*Tx` variants exist to compose. Calling them in sequence would leave a
  `draft` plan plus partial nodes on any mid-import failure or cancellation, and the retry
  would hit `ErrDuplicateSlug` instead of completing — a plan permanently undispatchable.
- **Test**: `go test ./internal/orch/ ./internal/store/ -race`; new
  `TestImportPlanLinearPlanMaterializesChainEdges`,
  `TestImportPlanExplicitAfterMaterializesEdges`, `TestImportPlanRejectsCycle`,
  and `TestImportPlanFailureLeavesNoPlanRow` (inject a failure after the plan insert;
  assert zero `plans` rows and that re-importing the same slug succeeds).
- **Depends on**: 3, 4.

---

### Increment 6 — dispatch walks the graph

- **Files (~5)**: `internal/orch/orchestrator.go`, `internal/orch/orchestrator_test.go`,
  `internal/orch/dispatch_verify_test.go`, `internal/orch/fanout_test.go`,
  `docs/api/internal/orch.md`
- **Change**: in `DispatchNext`, replace
  `readySteps, refs, parallel := plan.DecomposeRefs(parsedPlan, done)` with
  `readyTasks, err := o.store.Ready(ctx, planID)`. `materializeStepTask` collapses to a
  `GetTask` (import already materialized). `claimStepTask` calls `ClaimTask` and drops the
  *"ClaimNextReady claimed a DIFFERENT ready task"* fallback branch.
- **Native fan-out must stay group-scoped** — `parallel` must NOT derive from
  `len(readyTasks) > 1`. See the correction note below.
- **Test**: `go test ./internal/orch/ -race` — **every** `TestDispatchNext*` in §D must pass
  unchanged, plus a new case asserting a ready wave spanning two leaf groups with
  different bindings does NOT fan out as one group.
- **Depends on**: 5. **This is the increment §D guards.**

> **Correction — preserve leaf-group boundaries for native fan-out (Codex P1 on
> PR #210, accepted).**
>
> Deriving `parallel` from `len(readyTasks) > 1` is wrong. Verified against the
> tree: today `parallel` is a property of ONE leaf group (`decomposeGroups` in
> `internal/plan/decompose.go` returns it per-group), and `dispatchFanoutGroup`
> (`orchestrator.go:1144`) *"delegates an entire ready parallel step-group to ONE"*
> provider — resolving a single binding via
> `o.resolveBinding(ctx, projectID, parallel, …)` (`:469`) and using the first
> task's group heading in the fan-out prompt (`:388`).
>
> Under a DAG, a ready wave can legitimately contain tasks from *different* leaf
> groups with different bindings, team paths, and independence domains. Treating
> the whole wave as one fan-out group would hand unrelated tasks to a single
> provider under one group's heading — silently wrong dispatch, not just
> suboptimal scheduling.
>
> **Revised:** carry each task's leaf-group identity through import (the
> `task_metadata` row from increment 2 already persists team path and binding, so
> add the group path there rather than re-parsing markdown). In `DispatchNext`,
> partition the ready set by *compatible* group — same leaf group, same resolved
> binding, same independence domain — and apply native fan-out only within one
> partition. A partition of size 1, and any wave whose tasks do not share a
> group, dispatches one task at a time exactly as a non-parallel group does
> today. This keeps the degenerate case identical (§D.4 `fanout_test.go` must pass
> unchanged) while making the DAG case correct.

---

### Increment 7 — path containment + completion boundary

- **Files (~5)**: `internal/orch/task_paths.go` (new),
  `internal/orch/task_paths_test.go` (new), `internal/orch/verify.go`,
  `internal/orch/completion_boundary_test.go` (new), `internal/orch/verify_test.go`
- **Change**: `validateTaskFilesystem` / `verifyTaskCompletionFilesystem` /
  `secureProjectPath`, **with the §E.4 fix applied** — return the *resolved* path and
  re-validate at use. Wire the pre-dispatch input/output check into `DispatchNext`.
- **Test**: `go test ./internal/orch/ -race`; new
  `TestSecureProjectPathRefusesSymlinkPlantedAfterCheck`,
  `TestSecureProjectPathReturnsResolvedPath`.
- **Depends on**: 6.

---

### Increment 8 — output reservations

- **Files (~4)**: `internal/store/tasks.go` (`ErrOutputReserved` + reservation predicate in
  `ClaimTask`), `internal/store/tasks_test.go`, `internal/orch/plan_import.go` (persist
  reservations), `internal/orch/plan_import_test.go`
- **Test**: `go test ./internal/store/ ./internal/orch/ -race`; new
  `TestClaimTaskRefusesReservedOutput`.
- **Depends on**: 7.

---

### Increment 9 — provider invocation + capabilities

- **Files (~10)**: `internal/provider/invocation.go` (new),
  `internal/provider/capabilities.go` (new), `internal/provider/provider.go`,
  `internal/provider/codex.go`, `internal/provider/claude.go`,
  `internal/provider/opencode.go`, + tests, `docs/api/internal/provider.md`
- **Change**: `ResolveInvocation` / `Invocation`; `Binding.CalibratedCapabilities`,
  `Request.StrictBinding`, `Result.Invocation`. **Hand-apply onto main's post-#202 files.**
- **Test**: `go test ./internal/provider/ -race` — every #202 test
  (`TestCodex*`, `provider_v6/v7/v8_test.go`, `watchdog_test.go`) must pass unchanged.
- **Depends on**: nothing; can land any time after 1. Sequenced here because 10 needs it.

---

### Increment 10 — calibration

- **Files (~10)**: `internal/orch/calibration_admission.go`,
  `internal/orch/binding_constraints.go`, `internal/store/calibrations.go`,
  `internal/store/calibration_attempts.go`, `internal/supervisor/calibration.go`, + tests
- **Test**: `go test ./internal/orch/ ./internal/store/ ./internal/supervisor/ -race`
- **Depends on**: 9, 6.

---

### Increment 11 — IPC + clients

- **Files (~20)**: `internal/ipc/*`, `internal/tui/{team,team_view,model,meso,micro}.go`,
  `internal/gui/{team,views,app,live,controller}.go`,
  `cmd/radioactive_ralph/{binding_resolver,provider_cmd}.go`, + tests
- **Test**: `go test ./... -race`; then a real supervisor run per the DoD's "verify the app
  RUNS".
- **Depends on**: 10.

---

### Increment 12 — docs

- **Files (~12)**: `docs/guides/plan-format.md`, `docs/design/deterministic-execution.md`,
  `docs/design/exact-provider-identity.md`, `docs/design/plan-adaptive-concurrency.md`,
  `docs/design/index.md`, `docs/api/internal/*.md`, `docs/reference/architecture.md`,
  `README.md`, `.agent-state/decisions.ndjson`
- **Test**: doc-generation check + `go build ./...`
- **Depends on**: 11.

---

## D. Degenerate-case proof

The claim to prove: **once dispatch walks `task_deps`, a linear plan behaves identically.**
The proof is that these real tests on main pass **unchanged** — no edits to assertions, no
new fixtures, no relaxed expectations. Any one of them needing a change means the
degenerate case broke.

### D.1 Ordering semantics — `internal/plan/decompose_test.go`

Increment 4 touches `decompose.go`, so these guard that adding metadata parsing did not
perturb positional decomposition:

- `TestDecomposeSequentialGroupsGateOnEachOther`
- `TestDecomposeOrderedListReturnsOnlyFirstPending`
- `TestDecomposeUnorderedListReturnsAllPendingInParallel`
- `TestDecomposeRecursesIntoFirstIncompleteSubgroup`
- `TestStepRefIDStableAcrossReparse`
- `TestDecomposeRefsMatchesDecompose`
- `TestStepAtResolvesRef`, `TestStepAtOutOfRange`, `TestDecomposeEmptyPlan`

### D.2 Grammar — `internal/plan/plan_test.go`, `internal/plan/validate_test.go`

A plan with no ` ```ralph-task ` block must parse and validate byte-identically:

- `TestParseTwoTopLevelGroupsSequential`, `TestParseSubheadingsCarryOrderingDontDescend`
- `TestParseUnorderedListIsParallel`, `TestParseOrderedListIsSequential`
- `TestParseBulletsPlusParagraphIsOneStepWithDetail`, `TestParseLeafHeadingNoList`
- `TestParseThreeLevelNesting`, `TestParseHeadingLevelSkip`, `TestParseEmptyDocument`
- `TestParseApprovalMarker` — the `[approval]` marker must survive alongside metadata
- `TestParseUnknownBracketTokenIsNotAMarker` — guards against the metadata parser
  over-consuming
- `TestValidateCleanPlanHasNoFindings` — **the single strongest degenerate-case signal**: an
  un-annotated plan must produce zero findings after dependency validation joins
  `ValidateForImport`
- `TestValidateEmptyGroupFlagged`, `TestValidateLeadingParagraphBeforeListFlagged`,
  `TestValidateTrailingParagraphNotFlagged`,
  `TestValidateMixedOrderedUnorderedListsFlagged`, `TestValidateSubheadingsNotTreatedAsLeaf`

### D.3 Dispatch — `internal/orch/orchestrator_test.go` (increment 6's real gate)

These encode the exact behaviors the graph walk must reproduce:

- **`TestDispatchNextSequentialDispatchesOnlyFirstStep`** — the linear case. Under the
  graph walk, an ordered list materializes as a chain, so `store.Ready` returns exactly one
  task. If this passes without modification, linear-plan behavior is preserved.
- **`TestDispatchNextParallelDispatchesAllReadySteps`** — an unordered leaf materializes as
  N siblings sharing one predecessor, so `Ready` returns all N.
- `TestDispatchNextMaxParallelBoundsDispatch` — admission budget is downstream of readiness
  and must not shift.
- `TestDispatchNextMaxParallelScansPastGatedPrefix` — the `stepGateBlocks` scan-past
  behavior must survive the source swap.
- `TestDispatchNextHoldsGatedStepUntilApproved` — `ready_pending_approval` is excluded by
  `Ready`'s `status IN ('pending','ready')`; the gate must still hold.
- `TestDispatchNextNothingReadyReturnsZero` — empty `Ready` ⇒ zero, same as empty
  `DecomposeRefs`.
- `TestDispatchNextSpendCapRefusesDispatch`, `TestDispatchNextDoesNotBlockOnSlowProvider`,
  `TestDispatchSemaphoreBoundsInFlightTurns`, `TestDispatchHeartbeatsRunningWorker`,
  `TestDispatchWorkerPanicIsContained`, `TestKillWorkerCancelsInFlightRun`

### D.4 Fan-out — `internal/orch/fanout_test.go`

Native fan-out keys off group parallelism; when parallelism derives from `len(readyTasks)`
instead of the leaf flag, these must hold:

- `TestDispatchNextNativeFanoutDelegatesWholeGroupToOneWorker`
- `TestDispatchNextNonFanoutProviderDispatchesOneWorkerPerStep`
- `TestDispatchNextNativeFanoutOnlyAppliesToParallelGroups` — **the sharpest**: a
  *sequential* group must never take the fan-out path. Under the graph walk this means a
  chain must never yield >1 ready task.
- `TestDispatchNextNativeFanoutRunnerErrorFailsEveryTaskInGroup`
- `TestDispatchNextRalphManagedPoolDoesNotSkipProbeBinding`,
  `TestDispatchNextSaturatedProbeDoesNotConsumePoolCursor`,
  `TestDispatchNextApprovalProbeDoesNotConsumePoolCursor`

### D.5 Fail-closed completion — `internal/orch/dispatch_verify_test.go`, `verify_test.go`

These are the #202/`eb04193` guardrails; increment 7 touches `VerifyAndComplete`:

- **`TestDispatchNextWorkerTerminationAloneIsNotCompletion`**
- **`TestDispatchNextRunnerErrorMarksFailedNotDone`**
- `TestVerifyAndCompleteRejectsFailingAcceptanceCommand`,
  `TestVerifyAndCompleteAcceptsPassingAcceptanceCommand`,
  `TestVerifyAndCompleteRejectsMissingFile`,
  `TestVerifyAndCompleteJudgmentOnlyFallsBackToNonEmptyOutput`,
  `TestVerifyAndCompleteRetryExhaustionMarksFailed`
- `TestDefaultAcceptanceJSONDerivesCommand`,
  `TestDefaultAcceptanceJSONDerivesFileFromDetail`,
  `TestDefaultAcceptanceJSONNoAnnotationIsEmpty` — adding `RequiredOutputs` to `Acceptance`
  must not change what an un-annotated step derives.

### D.6 Store invariants — `internal/store/tasks_test.go`, `migrate_test.go`, `store_test.go`

- `TestCreateTaskAndReady`, `TestAddDepRejectsSelfAndCycle`,
  `TestClaimNextReadyAndMarkDone`, `TestClaimNextReadyConcurrentUniqueness`,
  `TestClaimNextReadyHonorsSequenceOrdinal`, `TestClaimNextReadyNoTasksInPlan`
- `TestMarkFailedRetriesThenTerminal`, `TestMarkBlocked`, `TestReleaseClaimDoesNotChargeRetry`
- `TestMarkFailedIgnoresStaleReportFromReclaimedSession`,
  `TestMarkDoneIgnoresStaleCompletionFromReclaimedSession` — must survive the
  `ErrTaskNotOwnedRunning` rewrap
- `TestApproveTask`, `TestApprovedReadyTaskIsClaimable`,
  `TestCreateTaskRequiresApprovalStartsGated`
- `TestStatusCounts` — must survive the `Blocked +=` fold
- `TestOpenRunsMigrations`, `TestRefuseNewerSchema`, `TestReopenIsIdempotent`,
  `TestForeignKeyCascade`, `TestApplyMigrationSuccessBumpsVersion`,
  `TestApplyMigrationExecFailureDoesNotBumpVersion`
- `TestConcurrentWritersDoNotLock`, `TestWriteSucceedsDuringBackup` — guard increments 1
  and 3

**Gate**: run `go test ./internal/plan/ ./internal/orch/ ./internal/store/ -race -count=1`
after every increment and diff the pass list against main's. A *changed test* is a
regression until proven otherwise.

---

## E. Discard list

### E.1 Codex invocation expansion — CONFIRMED, but the framing needs correcting

The brief describes an "18-argument Codex invocation". Main's `codex.go:80–96` builds a
compact argv:

```go
args := []string{
	"exec", "--json", "--color", "never", "--skip-git-repo-check",
	"--dangerously-bypass-approvals-and-sandbox",
	"-C", req.WorkingDir, "--output-last-message", outPath,
}
model := resolveModel(binding.Config, req.Model)
if model != "" { args = append(args, "-m", model) }
```

The archive's actual `codex.go` diff is small — it swaps `resolveModel` for
`ResolveInvocation` and adds `-c model_reasoning_effort=%q`. **But discard the file diff
anyway**, for a stronger reason: `eb04193` (#202) rewrote `codex.go` by +119/−... and added
`codex_diagnostics.go` (524 lines), `result_limits.go`, `codex_streaming_test.go`,
`codex_adversarial_review_test.go`. The archive's `codex.go` is built on the pre-#202 file.
Porting it as a diff will either conflict or silently revert #202's fail-closed
`ErrCodexOversizeSchema` / retained-line-budget work.

**Action**: discard the archive's `codex.go`, `claude.go`, `opencode.go` wholesale.
Hand-apply only two things onto main's current files: the `ResolveInvocation` call and
`Result.Invocation`. Same for `internal/provider/procgroup.go` (+23) — #202 rewrote
`killgroup_unix.go` (+138) and `killgroup_windows.go`; verify before porting, expect it to
be superseded.

### E.2 Anything that regresses #202's fail-closed hardening

The branch's merge-base is `c6eb5d1`; `eb04193` is **not** in its history. Everything it
touches under `internal/provider/` and `internal/agent/` is pre-#202. Discard by default;
re-derive by hand. The #202 test files that must keep passing:
`provider_v6_test.go`, `provider_v7_test.go`, `provider_v7_unix_test.go`,
`provider_v8_test.go`, `codex_test.go`, `codex_streaming_test.go`,
`codex_adversarial_review_test.go`, `watchdog_test.go`, plus `internal/agent/`'s
`lifecycle_test.go`, `lifecycle_exit_observer_test.go`, `lifecycle_agent_unix_test.go`.

### E.3 Raw runner-error persistence — one concrete instance found

`internal/store/plan_graph.go`'s `markMetadataBlocked` writes an arbitrary error string
straight into a durable column:

```go
UPDATE task_metadata SET blocked_reason = ? WHERE plan_id = ? AND task_id = ?
```

fed from `internal/orch/v2_dispatch.go`:

```go
if blockErr := o.store.MarkBlockedInput(ctx, planID, metadata.ID, err.Error()); ...
```

`err.Error()` here can carry the full `os.ReadFile` failure including absolute host paths.
Main's structured `EventPayload{Reason, Retryable, OperatorAction, ...}` exists precisely so
reasons are structured rather than scraped, and #202's direction was to stop trusting raw
runner output.

**Action**: keep `MarkBlockedCapability`/`MarkBlockedInput`, but have them take a
classified reason (a small enum plus a bounded operator-facing message) rather than
`err.Error()`. Land `blocked_reason` as the classified value; put the raw detail in the
event log via `EventPayload`, where retention and redaction already apply.

### E.4 CWE-22 path traversal in `secureProjectPath` — LOCATED, REPRODUCED, FIX SPECIFIED

**Location**: `internal/orch/v2_admission.go:76–107` on `archive/plan-v2-dag`
(introduced by `15035ae` "canonicalize portable task paths" and its follow-up `c9764f5`).

Amazon Q's flag is real, but **not** dot-dot traversal — I verified the ingress gate
`plan.validateRelativePath` (`internal/plan/v2.go`) correctly rejects `..`, `.`,
non-canonical spellings, absolute paths, and `\`/`:`:

```text
"a/../../etc/passwd"  -> not a canonical project-relative path
"sub/../../out"       -> not a canonical project-relative path
".."                  -> not a canonical project-relative path
```

The real defect is **symlink TOCTOU plus an unresolved return value**:

```go
	var resolved string
	if mustExist {
		resolved, err = filepath.EvalSymlinks(candidate)
	} else {
		resolved, err = resolveThroughExistingAncestor(candidate)
	}
	if err != nil { return "", fmt.Errorf("resolve symlinks: %w", err) }
	if !pathContained(root, resolved) {
		return "", fmt.Errorf("symlink escapes project root")
	}
	if mustExist { return resolved, nil }
	return candidate, nil     // <-- validated `resolved`, returns `candidate`
```

On the `mustExist=false` branch it validates `resolved` and then **returns the unresolved
`candidate`**. Every caller therefore writes through whatever the path resolves to *at write
time*, not at check time. Combined with the fact that outputs are checked at
`ImportPlan` time (`validateV2Filesystem` in `plan_import.go`) but written much later by a
worker, a peer task in the same plan can plant a symlink in between.

I reproduced the escape end-to-end:

```text
=== VECTOR 1: TOCTOU — validate at import, plant symlink, write at dispatch ===
import-time output check: returned=".../project/build/out.txt" err=<nil>
  outside/out.txt content="ESCAPED" err=<nil>  -> ESCAPE=true
```

A task declaring output `build/out.txt` passes admission while `build/` does not yet exist;
a peer task then creates `build` → `/etc` (or any absolute path); the write lands outside
the project root. This is CWE-22 (and CWE-367).

**Fix — three parts, all in increment 7's `internal/orch/task_paths.go`:**

1. **Return the resolved path, never the candidate.** Delete `return candidate, nil`; return
   `resolved` on both branches. Callers must operate on the containment-checked path.
2. **Re-validate at use, not only at admission.** `validateTaskFilesystem` at import is a
   fast-fail, not the security boundary. Re-run `secureProjectPath` immediately before the
   dispatch that will write, and again in `verifyTaskCompletionFilesystem` — which the
   branch already does for the completion side.
3. **Read through a build-tagged no-follow helper.** For the file-existence and hash-pin
   reads, open with no-follow semantics and hash from the returned `*os.File`, so the bytes
   checked are the bytes of the inode opened rather than a path re-resolved afterward.
   `os.ReadFile(path)` in `validateV2Inputs` re-resolves and is racy.

   `syscall.O_NOFOLLOW` is **Unix-only** and must not appear in portable code — Ralph
   targets macOS, Linux, and Windows. The repository already ships exactly the right
   pattern in `internal/provider/result_open_{unix,windows,unsupported}.go`:

   - `//go:build darwin || linux` → `unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK`
   - `//go:build windows` → `windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT`
   - `//go:build !darwin && !linux && !windows` → **fails closed**, no pathname fallback

   Reuse that trio's shape rather than inventing a second one, and keep the
   fail-closed default: a platform without a safe open must refuse the read, never
   degrade to `os.ReadFile`.

Additionally, tighten `resolveThroughExistingAncestor`: it walks up to the first existing
ancestor and re-joins the non-existent suffix, so a symlink created *in that suffix gap*
after the check is invisible.

> **Correction — (1)–(3) do NOT close the write-time race (Codex P1 on PR #210,
> accepted).**
>
> The paragraph above originally claimed the residual window is closed by
> re-running containment immediately before the write. That is wrong, and the
> distinction is important enough to state plainly rather than leave as an
> implied guarantee.
>
> Steps (1)–(3) constrain **Ralph's own reads**: `O_NOFOLLOW` + hashing from the
> `*os.File` means the bytes Ralph checks are the bytes of the inode it opened.
> That is worth doing and those tests should land.
>
> They do **not** constrain the provider. The provider is a separate process that
> later performs its own pathname-based `open("build/out.txt", O_CREAT|O_WRONLY)`.
> A peer can replace a nonexistent suffix component, or a parent directory, with a
> symlink *after* Ralph's pre-dispatch check returns and *before* the provider
> writes. Returning a resolved string from `secureProjectPath` does not travel into
> the provider's syscalls, and revalidating "immediately before dispatch" only
> shrinks the window — it cannot eliminate it, because the write happens minutes
> later inside another process.
>
> **Therefore:** treat declared-output containment as **best-effort validation, not
> a security boundary**, and document it as such. Ralph's guarantee is
> "a declared output that escapes the project root is *detected* at completion"
> (`verifyTaskCompletionFilesystem` with `O_NOFOLLOW`), not "a provider cannot
> write outside the project root." A real write-side guarantee requires an actual
> containment primitive around the provider process — a sandbox profile, mount
> namespace, or brokered file API — which is a separate design with its own
> platform matrix (Ralph supports macOS, Linux, and Windows) and is explicitly out
> of scope for the DAG integration. File it as follow-up work rather than implying
> the path checks already deliver it.
>
> The CWE-22 fix in (1) remains necessary and sufficient for what it does claim:
> refusing an absolute path outright instead of honoring it, and failing closed on
> a resolution error.

**Regression tests to add** (`internal/orch/task_paths_test.go`):
`TestSecureProjectPathReturnsResolvedPath`,
`TestSecureProjectPathRefusesSymlinkPlantedAfterCheck`,
`TestValidateTaskInputsRefusesSymlinkSwapDuringHash`.

### E.5 Double filesystem verification in `VerifyAndComplete`

The branch's `verify.go` calls `verifyV2CompletionFilesystem` twice — once before
`acceptanceCheck` and once after, with the second producing *"v2 contract changed during
acceptance"*. This is a race *detector*, not a race *fix*: it narrows the window without
closing it, and doubles the filesystem cost of every completion. Discard the second call.
Discarding the second call loses nothing §E.4 provides, because the two address
different things. §E.4 (resolved paths + no-follow opens) hardens **Ralph's own reads
and completion checks** — the bytes Ralph hashes are the bytes of the inode it opened.
It does **not** close the provider write-side race, which stays out of scope per the
correction in §E.4: a separate process writing through a pathname minutes later is
outside anything Ralph can enforce from its own address space. The discarded second
check narrowed that window without closing it either, at double the filesystem cost.

### E.6 `Plan.V2 bool` and every `parsed.V2` fork

`internal/plan/types.go` (branch) `Plan.V2`, `orch/plan_import.go`'s
`if !parsed.V2 { return o.importLegacyPlan(...) }`, and `importLegacyPlan` itself. A
document-level version flag switching between two execution contracts is the exact
duplication the reland exists to remove. Absence of `after:` means "edges come from
document order", not "different engine".

### E.7 `store/plan_graph.go`'s `CreatePlanGraph` and `store/graph_validate.go`

`CreatePlanGraph` (417-line file) re-implements `CreatePlan` + `CreateTask` + `AddDep` with
raw SQL inside one transaction — `insertGraphPlan` duplicates `CreatePlan`'s INSERT verbatim
including `ErrDuplicateSlug` mapping; `insertGraphTask` duplicates `CreateTask`'s INSERT and
`RequiresApproval` status logic. Worse, it INSERTs into `task_deps` directly, **bypassing
`AddDep`'s `wouldCreateCycle` check** — the only runtime cycle guard in the store. Discard
both; compose the three existing methods in `orch.ImportPlan`. Keep the *idea* of atomicity
by wrapping the composition, not by re-writing the SQL.

### E.8 `Task` struct widening + `enrichTaskMetadata` + `store/task_metadata_view.go`

Adding 11 provenance fields to `Task` and firing a `LEFT JOIN task_metadata` on every
`GetTask`/`ListTasks` taxes the hottest read path in the system — `DispatchNext` calls
`ListTasks` once per pass and `GetTask` per step. Main documents `Task` as "a DAG node";
keep it that way. Provenance-needing callers call `GetTaskExecutionMetadata` explicitly.
`task_metadata_view.go` (139 lines) exists only to serve this widening — discard.
(`ListRunningWorkers`'s JOIN in `workers.go` is fine: operator-facing, low frequency.)

### E.9 Every `v2` / `V2` identifier, filename, symbol, and test name

Non-negotiable per the repo's "versions are release-please's job" rule. Concretely:

| Discard | Use |
|---|---|
| `internal/plan/v2.go`, `v2_validate.go`, `v2_test.go` | `types.go`, `validate.go`, `plan_test.go` / `validate_test.go` |
| `internal/orch/v2_admission.go`, `v2_dispatch.go` | `task_paths.go`; dispatch folds into `orchestrator.go` |
| `internal/orch/v2_admission_test.go`, `v2_dispatch_test.go`, `plan_import_v2_test.go`, `v2_completion_boundary_test.go` | `task_paths_test.go`, `orchestrator_test.go`, `plan_import_test.go`, `completion_boundary_test.go` |
| `internal/store/plan_v2_lifecycle_test.go`, `plan_v2_scheduling_test.go` | fold into `tasks_test.go`, `task_metadata_test.go` |
| `internal/store/schema/0003_plan_v2.up.sql` | `0003_plan_graph.up.sql` |
| `docs/design/plan-v2-adaptive-concurrency.md` | `docs/design/plan-adaptive-concurrency.md` |
| `ValidateV2`, `V2Task`, `V2Tasks`, `dispatchNextV2`, `validateV2Bindings`, `validateV2Filesystem`, `validateV2Inputs`, `verifyV2CompletionFilesystem`, `strictV2AcceptanceJSON`, `convergeV2PlanTx`, `Plan.V2` | `ValidateForImport` (merged), `TaskRef`, `Tasks`, folded into `DispatchNext`, `validateTaskBindings`, `validateTaskFilesystem`, `validateTaskInputs`, `verifyTaskCompletionFilesystem`, `strictAcceptanceJSON`, `convergePlanTx`, (deleted) |
| `store.exactClaimGate`, `ClaimReadyTask`, `exactClaimBusyRetry*` | `claimGate`, `ClaimTask`, `claimBusyRetry*` |
| `ralph.plan/v2` in prose | "the plan task contract" |

The ` ```ralph-task ` fence label and the `taskMetadataLanguage` constant are **kept** —
they name the block's content type, not a version.

### E.10 Windows named-pipe shutdown (`008d752`) — verify before porting

`008d752` "fix(ipc): bound Windows named-pipe shutdown" touches
`internal/ipc/transport_windows.go` and `server_windows_test.go`. Per `CLAUDE.md`, the
architecture spec's Windows clauses are **superseded** by
`docs/superpowers/specs/2026-07-26-windows-scm-safety-disable-design.md`, which postdates
this commit. Read that contract before porting; the fix may target a code path the SCM
safety design disables outright.

---

## Summary of the shape change

| | Source branch | Integrated design |
|---|---|---|
| Plan types | `plan/v2.go` beside `parse.go` | `plan/types.go` + `Step.Metadata` |
| Validation | `plan/v2_validate.go` beside `validate.go` | `validate.go` / `ValidateForImport` |
| Edges | `store/plan_graph.go` raw INSERT | existing `task_deps` via `AddDep` |
| Cycle check | branch DFS + bypassed store check | `plan.dependencyCycle` (document) + `store.wouldCreateCycle` (persistence) |
| Dispatch | `orch/v2_dispatch.go` beside `orchestrator.go` | `DispatchNext` sources from `store.Ready` |
| Linear plan | separate `importLegacyPlan` + `DecomposeRefs` path | chain of edges — the degenerate DAG, one path |
| Migration | `0003_plan_v2.up.sql` | `0003_plan_graph.up.sql` |
| Live code paths | two | one |
