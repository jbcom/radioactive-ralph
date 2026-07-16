# radioactive-ralph: A2A Comms & Plan Storage — Architecture Decision

## 1. A2A COMMS: Build our own thin A2A-shaped protocol over `internal/ipc` + the SQLite DB

**Decision: REJECT a2abridge (both as library and as sibling service). Build in-house.**

### Why not a2abridge

The evaluation kills it on fit before it even gets to maturity/risk:

- **Not embeddable at all.** Every package lives under `internal/`, which is a hard Go-compiler boundary, not a style choice. "Adopt as a library" isn't on the table without forking upstream and relocating `a2a/` out of `internal/` — at which point we're not adopting a dependency, we're adopting a maintenance burden against someone else's spec-compliance churn.
- **Completion semantics are inverted from our requirement.** Its `a2a_complete_task` MCP tool lets an agent flip its own task to `COMPLETED`. Our whole team-lead/worker model exists specifically so completion is **orchestrator-verified, never agent-asserted**. This isn't a minor adaptation — it's the opposite trust model. Reusing the Task/TaskState shape while inverting who's allowed to call the transition is realistically a rewrite of the state machine, not a reuse of it.
- **Zero durable storage, and we must own durability anyway.** In-memory task map, 30-min TTL janitor, 90s TTL directory eviction, one best-effort JSON snapshot file. Adopting it buys us nothing on the "one user-level SQLite DB" requirement — we'd still build 100% of our own durability layer on top.
- **Wrong lifecycle unit.** Its concurrency model is "one OS process per IDE/agent window, bridge dies with the session." Our model is a durable service supervising worker Ralphs across long-running plan execution. Even the parts that are reusable in principle need semantic adaptation, not an import.

### What we take from it conceptually (not as code)

The A2A 1.0 *shape* — agent cards, a Task with a bounded state machine, Message/Part discriminated unions, JSON-RPC method naming — is a reasonable reference vocabulary. It costs nothing to mirror that shape in our own Go types (e.g., our own `Task{State, Parts, ...}` enum matching A2A's state names) so we're "standardized" in spirit and could later expose a real A2A-compliant surface at the edge if we ever need external interop. That's a naming/shape decision, not a dependency decision.

### What we build

A thin internal protocol layered on what already exists:
- **Transport:** extend `internal/ipc` (already our socket protocol) with A2A-shaped message types: task assignment, status query, result submission (as a *claim*, not a completion assertion), cancellation.
- **Durability:** task/message log lives in the same one user-level SQLite DB as everything else — new tables (`a2a_tasks`, `a2a_messages`), not a second store.
- **Trust model:** worker Ralphs submit *evidence of completion* (e.g., "I ran X, here's the exit code/output/diff"); only the orchestrator (team-lead Ralph, or the runtime service on its behalf) transitions a task to a terminal `done` state after verifying that evidence against the plan DAG's actual done-criteria. This is the non-negotiable property a2abridge cannot give us regardless of integration shape.
- **Scope:** local-machine, multi-Ralph-process coordination only — not a general internet-facing A2A server. No need for the mDNS discovery, push-webhook extension, or SSE streaming machinery a2abridge carries for the "multiple human-launched IDE windows" use case.

**Strongest risk to flag:** building our own means we own spec drift and edge cases (cancellation races, duplicate task claims, worker crash mid-task) that a battle-tested library would otherwise absorb. Mitigate by keeping the state machine intentionally small (fewer states than full A2A 1.0) and covering it with the same rigor as `internal/plandag` — this is a case where "smallest surface that satisfies orchestrator-verified completion" beats copying A2A's full spec surface.

---

## 2. PLAN STORAGE/FORMAT: CONFIRM the goldmark + validated-markdown + SQLite plan; vectors are OUT

**Decision: CONFIRM the plan as designed. ADOPT goldmark. REJECT sqlite-memory and sqlite-sync outright — not "later," not "with caveats."**

### goldmark: adopt, straightforward fit

- Pure Go, zero transitive dependencies, MIT, no CGO, unaffected by `CGO_ENABLED=0` — fully compatible with the modernc.org/sqlite pure-Go toolchain and single-static-binary distribution goal.
- The AST shape matches the plan spec almost exactly: `ast.Heading{Level}` as flat siblings, `ast.List.IsOrdered()` (via `Marker`) as the parallel/sequential switch, `ast.ListItem` as steps. "Heading level = nesting group, don't descend past a heading with subheadings" is precisely a sibling-scan-with-lookahead (`NextSibling()` until a `Heading` with `Level <= current`), which goldmark's linked-list node structure supports natively.
- **One real gap to own ourselves:** goldmark does not group a heading's "section" for you — headings are flat siblings, not auto-nested. We write and own that stop-at-next-heading-level-≤N loop. Small, contained, and it's exactly the "heuristic decomposition" logic that's supposed to be ours anyway, not a library's.
- **One ambiguity to resolve in the validator, not the parser:** a heading with both a bare paragraph and a list directly under it (no subheading) is a case the plan-format spec doesn't disambiguate today. Goldmark hands back both node kinds as siblings with no opinion — pin down precedence (e.g., "a list present under a heading means the heading is a step-group; a bare paragraph is just narrative/notes") in the plan-format validator before this becomes a real plan authored in the wild trips it.
- Use `goldmark.DefaultParser()` scoped to core block/inline parsing only — skip `goldmark.New()` with `extension.GFM` or friends. Tables/strikethrough/footnotes etc. aren't part of the validated grammar and pulling them in only widens the surface a plan author could accidentally use in a way the orchestrator can't interpret.
- Use `*ast.Text.Segment.Value(source)` / `Paragraph.Lines().Value(source)` for text extraction — not the deprecated `Node.Text(source)` convenience method.

Storage: parse transiently at load/update time, compute pending/next state from the AST + done-state, persist the *result* (not the AST) into the existing plan schema in the one SQLite DB. Goldmark never touches storage, so there's no conflict with modernc.

### sqlite-memory: reject, hard blocker plus wrong architecture

Two independent, each-sufficient reasons to rule it out:
1. **CGO incompatibility is absolute.** It's a native C loadable extension requiring `sqlite3_load_extension`, which modernc.org/sqlite (pure-Go) does not implement at all. The only Go integration path shown (`cli/` module) requires switching to `mattn/go-sqlite3` (CGO) — i.e., abandoning the pure-Go storage engine entirely just to load this. Not a dependency add; a storage-engine swap.
2. **Wrong architecture even if CGO weren't a problem.** It's hybrid FTS5+vector semantic memory with an embedding pipeline (local llama.cpp/GGUF or remote API). We explicitly don't want vector/RAG for plans — heuristic AST decomposition is the whole point, precisely because it's deterministic and inspectable, where embedding-based recall is brittle and non-deterministic for something as structural as "what's the next step."

License is a distant third concern (Elastic License 2.0, commercial-use gate) — moot given the technical blockers above.

### sqlite-sync: reject, same CGO blocker, wrong problem entirely

Same hard incompatibility (native C extension, no CGO in our stack) plus it solves a different problem we don't have yet: multi-device CRDT replication of a whole DB, not multi-agent-on-one-machine coordination. Its merge-truth CRDT model (Delete-Wins, Add-Wins, etc.) is actively the wrong consistency model for orchestrator-verified completion — merge semantics would fight, not support, "orchestrator decides, never agent-asserted." Worth a second look only if a genuine multi-machine sync requirement appears later, and even then it requires reintroducing CGO as a storage engine — a much bigger call than "add a sync library."

### Vectors: confirmed out

Restating explicitly since the brief asked: yes, vectors/RAG are out, full stop, for plan storage and decomposition. Nothing in this evaluation set changes that — if anything, sqlite-memory's eval reinforces the original skepticism (semantic recall solves "find something related," not "compute the next unambiguous step from a validated grammar," which heuristic AST parsing does deterministically and auditably). If a future need for fuzzy retrieval over historical plans/logs emerges, treat it as a distinct, separately-justified feature — not a reason to revisit this plan-execution path.

---

## Summary table

| Decision | Verdict | Decisive factor |
|---|---|---|
| a2abridge as library | Reject | `internal/` boundary — not importable without a fork |
| a2abridge as sibling service | Reject | Agent-asserted completion contradicts orchestrator-verified requirement; zero durability gained |
| A2A comms approach | Build in-house on `internal/ipc` + SQLite | Only way to keep orchestrator-verified completion + single-DB durability; reuse A2A vocabulary only as naming inspiration |
| goldmark | Adopt | Pure Go, zero deps, AST shape matches heuristic-decomposition spec almost exactly |
| sqlite-memory | Reject | CGO/modernc incompatible (hard blocker) + wrong architecture (vector/RAG we don't want) |
| sqlite-sync | Reject | Same CGO blocker + wrong consistency model (CRDT merge-truth vs. orchestrator-truth) |
| Vectors/RAG for plans | Confirmed out | Heuristic AST-over-validated-markdown is deterministic and auditable; embeddings are brittle for structural "what's next" queries |

---

> **Addendum (2026-07-16):** this eval studied the third-party `a2abridge`. The official `a2aproject/a2a-go` SDK (Apache-2.0, importable packages, plain TaskState enum, stdlib-only core types) was subsequently adopted for the A2A vocabulary instead — see `.agent-state/decisions.ndjson` (a2a-comms-layer). The 'own durability + orchestrator-verified completion' conclusion stands; only the A2A *types* come from the official SDK rather than being hand-rolled.
