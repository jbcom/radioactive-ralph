# Supervisor Architecture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild radioactive-ralph as a supervised-execution runtime: one binary with a `--supervisor` mode that owns every agent's pty (never letting an agent block), a dumb client that discovers the supervisor and renders a read-only TUI, and one user-level SQLite database as durable memory for all projects.

**Architecture:** A single Go binary. `--supervisor` runs a long-lived process that owns agent subprocesses via `creack/pty`, watches each for stall/prompt/resource-limit and kills-and-reclaims (never waits), and persists everything to one XDG-located SQLite DB. The plain client refuses to run without a discoverable supervisor (the socket at an XDG runtime path IS the advertisement), and renders a read-only macro/meso/micro Bubble Tea TUI. Providers are local-only (claude/codex/opencode). This is a clean-slate rewrite on `feat/supervisor-architecture`; the old durable-service / per-repo-plandag / committed-config-dir model is removed.

**Tech Stack:** Go 1.26; `creack/pty` (NEW — agent pty ownership); `charmbracelet/bubbletea` + `lipgloss` (TUI, present); `spf13/cobra` + `spf13/viper` (NEW — CLI + layered config, replacing `alecthomas/kong`); `yuin/goldmark` (NEW — plan-markdown AST); `a2aproject/a2a-go` (NEW — A2A vocabulary/types, stdlib-only core); `modernc.org/sqlite` (pure-Go, present); `adrg/xdg` + `internal/xdg` (present); `jonboulle/clockwork` (test clock, present). No binary-size constraint — heavier deps are fine; pure-Go SQLite is a build-compat choice only.

## Global Constraints

- **Control invariant (non-negotiable):** an agent CLI must never block the system. All agents run non-interactively under Ralph's pty ownership; any stall/prompt/no-output → auto-resolve, deny, or kill-and-reclaim — never wait.
- **Local-only providers:** claude, codex, opencode (+ agy pending U-spike). No cloud control surface in the loop; hosted model inference is fine. No gemini, no cursor-agent.
- **One user-level SQLite DB** (XDG data dir) for all projects. No per-repo DB, no committed `.radioactive-ralph/` dir. Repos stay clean by default.
- **Project identity = accumulated fingerprints** (git heuristics + absolute-path seed), never fragile absolute paths alone.
- **Config = virtual merge layers** (§5a of the spec): 3 flags (`--config-file`/`-C`, `--user-config-file`, `--project-config-file`), USER layer = DB < `--config-file` < `--user-config-file`, PROJECTS layer = all-DB-projects < user-config `projects:` stanza; change (wizard/`--init`, persisted) vs override (normal mode, runtime-only); supervisor diffs and warns on conflicting user-level `projects:` overrides.
- **No variants / no personas.** One mutating Ralph; prompts are "you're an agent + task + context", not roleplay. `internal/variant` is deleted, not audited.
- **Completion is orchestrator-verified**, never agent-asserted and never inferred from termination.
- **Plans are simple markdown**, decomposed heuristically over the goldmark AST — no LLM, no structured output, no vectors.
- **CLI = cobra, config = viper** (replacing kong); A2A vocabulary from `a2aproject/a2a-go`.
- **Every terminal state write is error-checked and logged** (no silent orphaning — the class fixed in PR #63).
- **TDD:** failing test → run-fail → minimal impl → run-pass → commit, per task.
- **Green checkpoints:** because the branch is a mid-flight rewrite, each task must leave `go build ./...` + its own tests green even if the whole system isn't wired yet.
- **Reference spec:** `docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md`. **Decision rationale:** `.agent-state/decisions.ndjson`.

---

## Phasing overview

The rewrite proceeds in phases; each phase is a coherent, independently-green
increment. Phase 1 (foundation) is fully step-detailed below. Phases 2–8 are
task-outlined with their deliverables, files, and interfaces defined; each
phase's step-level TDD detail is expanded just-in-time at the start of that
phase (this avoids a large speculative artifact drifting from the code that
Phase 1 establishes). This just-in-time expansion is a deliberate, recorded
choice — see `.agent-state/decisions.ndjson` (migration-strategy) — not a
placeholder.

- **Phase 1 — Foundation:** `creack/pty` dep; the pty-owned agent process
  (`internal/agent`); the never-block watchdog. The control invariant, provable
  in isolation.
- **Phase 2 — User store:** the single XDG SQLite DB (`internal/store`) with the
  plan-DAG schema migrated in, project-identity fingerprints, spend, process
  tracking, session/role history. Backups.
- **Phase 3 — Config resolution:** the virtual-layer config engine
  (`internal/vconfig`): 3 flags, 2 layers, change-vs-override, conflict diff,
  merged validation with actionable missing-field errors.
- **Phase 4 — Supervisor + discovery:** `--supervisor` mode, socket-at-XDG
  advertisement, single-instance + stale reclaim, the dumb client's discover-or-
  refuse handshake (repurpose `internal/ipc` + `flock`).
- **Phase 5 — Providers + detection:** local-only provider bindings
  (claude/codex/opencode) on the new agent runtime, each a **capability record**
  (incl. native subagent/workflow/parallelism flag) — no personas; the `agy`
  spike; install/first-run detection classifying supported/deprecated/remote
  CLIs.
- **Phase 6 — Plan engine + orchestration (no variants):** `internal/plan`
  (goldmark AST + heuristic decomposition — heading=group, unordered=parallel,
  ordered=sequential, don't descend past a heading with subheadings + format
  validator); `internal/orch` (team-lead/orchestrator: dispatch next step with
  plan-scoped context, **orchestrator-verified completion**, enforcement-prompt
  + kill/restart context discipline, per-agent XDG decision logs absorbed into
  history); A2A vocabulary via `a2aproject/a2a-go` types over the user DB
  (`a2a_tasks`/`a2a_messages`). **`internal/variant` is deleted, not audited.**
- **Phase 7 — TUI + planning genesis:** read-only macro/meso/micro Bubble Tea
  view subscribing to the supervisor's live stream + DB scrollback; the planning
  genesis flow (agent-juxtaposition refinement → markdown doc in headless; render
  + `$EDITOR`/embedded-editor review in TUI; skip-planning path).
- **Phase 8 — E2E + teardown:** CI-feasible E2E (cassette agents) + local real-
  agent E2E under a spend cap using the reference fixtures (the orchestrator
  dispatching workers against a real plan under CLI-health observation); delete
  all dead old-model code (incl. `internal/variant`, kong); docs sweep; CI
  gating (branch protection + CodeQL-Go).

---

## Phase 1 — Foundation: pty-owned agent + never-block watchdog

Goal: prove the control invariant in isolation. An agent subprocess runs under
Ralph's pty ownership; Ralph reads its output as a stream, detects a stall/
prompt/no-output condition, and kills it — never blocking. No DB, no supervisor,
no TUI yet.

**File structure (Phase 1):**
- Create `internal/agent/agent.go` — the `Agent` type: owns one pty-backed
  subprocess; exposes output stream, structured-result fd, `Kill`, health.
- Create `internal/agent/watchdog.go` — the `Watchdog`: consumes the output
  stream + process health, emits `Signal`s (Progress/Stall/Prompt/Exited/
  ResourceExceeded), decides Kill.
- Create `internal/agent/agent_test.go`, `internal/agent/watchdog_test.go`.
- Modify `go.mod` — add `github.com/creack/pty`.

### Task 1: Add the creack/pty dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/creack/pty@latest`

- [ ] **Step 2: Verify it resolves and the module still builds**

Run: `go build ./... && go mod tidy && git diff --stat go.mod go.sum`
Expected: `go.mod` gains a `github.com/creack/pty` require line; build succeeds.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add creack/pty for agent pty ownership"
```

### Task 2: The pty-owned Agent — spawn, stream, kill

**Files:**
- Create: `internal/agent/agent.go`
- Test: `internal/agent/agent_test.go`

**Interfaces:**
- Produces:
  - `type Agent struct { ... }`
  - `type Options struct { Command string; Args []string; Dir string; Env []string; ResultPath string }`
  - `func Start(ctx context.Context, opts Options) (*Agent, error)` — starts the command under a pty via `pty.Start`.
  - `func (a *Agent) Output() <-chan []byte` — line-oriented output stream (a copy of the ptmx reader; safe to consume from one goroutine).
  - `func (a *Agent) Kill() error` — `Process.Kill()` then release the pty.
  - `func (a *Agent) Wait() error` — waits for exit, returns exit error.
  - `func (a *Agent) PID() int`
  - `func (a *Agent) Done() <-chan struct{}` — closed when the process exits.

- [ ] **Step 1: Write the failing test — a command runs under a pty and streams output**

```go
package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAgentStreamsOutputAndExits(t *testing.T) {
	ctx := context.Background()
	a, err := Start(ctx, Options{Command: "sh", Args: []string{"-c", "printf 'hello\\nworld\\n'"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	var got strings.Builder
	timeout := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-a.Output():
			if !ok {
				goto done
			}
			got.Write(line)
		case <-timeout:
			t.Fatal("timed out reading agent output")
		}
	}
done:
	if !strings.Contains(got.String(), "hello") || !strings.Contains(got.String(), "world") {
		t.Fatalf("output = %q, want hello+world", got.String())
	}
	if a.PID() <= 0 {
		t.Errorf("PID = %d, want > 0", a.PID())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentStreamsOutputAndExits -v`
Expected: FAIL — package/`Start` undefined.

- [ ] **Step 3: Implement `internal/agent/agent.go`**

```go
// Package agent runs a single AI-agent CLI subprocess under Ralph's own
// pty, so Ralph owns its stdio and can stream, control, and kill it. The
// developer never touches this terminal — Ralph does, as the control layer.
package agent

import (
	"bufio"
	"context"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// Options configures one agent subprocess.
type Options struct {
	Command    string
	Args       []string
	Dir        string
	Env        []string
	ResultPath string // file the CLI is told to write its structured result to
}

// Agent is a pty-owned agent subprocess.
type Agent struct {
	cmd    *exec.Cmd
	ptmx   *os.File
	out    chan []byte
	done   chan struct{}
	opts   Options
}

// Start launches opts.Command under a pty and begins streaming its output.
func Start(ctx context.Context, opts Options) (*Agent, error) {
	cmd := exec.CommandContext(ctx, opts.Command, opts.Args...)
	cmd.Dir = opts.Dir
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	a := &Agent{
		cmd:  cmd,
		ptmx: ptmx,
		out:  make(chan []byte, 256),
		done: make(chan struct{}),
		opts: opts,
	}
	go a.readLoop()
	return a, nil
}

func (a *Agent) readLoop() {
	defer close(a.out)
	defer close(a.done)
	scanner := bufio.NewScanner(a.ptmx)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		line = append(line, '\n')
		select {
		case a.out <- line:
		default:
			// Never block the reader on a slow consumer; drop to keep
			// the pty draining (the watchdog only needs recent signal).
		}
	}
	_ = a.cmd.Wait()
	_ = a.ptmx.Close()
}

// Output is the line-oriented output stream; closed when the process exits.
func (a *Agent) Output() <-chan []byte { return a.out }

// Done is closed when the process exits.
func (a *Agent) Done() <-chan struct{} { return a.done }

// Kill terminates the process immediately and releases the pty.
func (a *Agent) Kill() error {
	if a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
	}
	return a.ptmx.Close()
}

// Wait blocks until the process exits.
func (a *Agent) Wait() error { <-a.done; return nil }

// PID returns the subprocess PID (0 before start / after release).
func (a *Agent) PID() int {
	if a.cmd.Process == nil {
		return 0
	}
	return a.cmd.Process.Pid
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestAgentStreamsOutputAndExits -v`
Expected: PASS.

- [ ] **Step 5: Write the failing test — Kill terminates a long-running agent**

```go
func TestAgentKillTerminates(t *testing.T) {
	a, err := Start(context.Background(), Options{Command: "sh", Args: []string{"-c", "while true; do sleep 1; done"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := a.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case <-a.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not exit after Kill")
	}
}
```

- [ ] **Step 6: Run to verify pass** (implementation already covers it)

Run: `go test ./internal/agent/ -run TestAgentKill -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): pty-owned agent subprocess with output stream and kill"
```

### Task 3: The never-block Watchdog

**Files:**
- Create: `internal/agent/watchdog.go`
- Test: `internal/agent/watchdog_test.go`

**Interfaces:**
- Consumes: `Agent.Output()`, `Agent.Done()` (Task 2).
- Produces:
  - `type SignalKind int` with `Progress`, `Stall`, `Prompt`, `Exited`, `ResourceExceeded`.
  - `type Signal struct { Kind SignalKind; Detail string }`
  - `type WatchdogConfig struct { StallTimeout time.Duration; PromptPatterns []*regexp.Regexp }`
  - `func Watch(ctx context.Context, a *Agent, cfg WatchdogConfig) <-chan Signal` — emits a `Prompt` signal the moment a prompt pattern matches a line, a `Stall` signal after `StallTimeout` with no output, `Progress` on each non-prompt line, and `Exited` when the agent exits. The caller decides to `Kill` — the watchdog never waits.

- [ ] **Step 1: Write the failing test — a prompt pattern fires a Prompt signal fast**

```go
package agent

import (
	"context"
	"regexp"
	"testing"
	"time"
)

func TestWatchdogDetectsPrompt(t *testing.T) {
	a, err := Start(context.Background(), Options{
		Command: "sh",
		Args:    []string{"-c", "printf 'working...\\nDo you want to proceed? (y/n)\\n'; sleep 5"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = a.Kill() }()
	sigs := Watch(context.Background(), a, WatchdogConfig{
		StallTimeout:   3 * time.Second,
		PromptPatterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\(y/n\)|proceed\?`)},
	})
	deadline := time.After(4 * time.Second)
	for {
		select {
		case s := <-sigs:
			if s.Kind == Prompt {
				return // detected the block before the 5s sleep would hang us
			}
		case <-deadline:
			t.Fatal("watchdog did not emit Prompt for a (y/n) line")
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agent/ -run TestWatchdogDetectsPrompt -v`
Expected: FAIL — `Watch`/`Prompt` undefined.

- [ ] **Step 3: Implement `internal/agent/watchdog.go`**

```go
package agent

import (
	"context"
	"regexp"
	"time"
)

// SignalKind classifies a watchdog observation.
type SignalKind int

const (
	Progress SignalKind = iota
	Stall
	Prompt
	Exited
	ResourceExceeded
)

// Signal is one watchdog observation about an agent.
type Signal struct {
	Kind   SignalKind
	Detail string
}

// WatchdogConfig tunes stall and prompt detection.
type WatchdogConfig struct {
	StallTimeout   time.Duration
	PromptPatterns []*regexp.Regexp
}

// Watch observes an agent and emits Signals. It NEVER blocks waiting on the
// agent: a prompt pattern or a stall is surfaced immediately so the caller
// can kill-and-reclaim. The channel closes when the agent exits.
func Watch(ctx context.Context, a *Agent, cfg WatchdogConfig) <-chan Signal {
	out := make(chan Signal, 16)
	go func() {
		defer close(out)
		timer := time.NewTimer(cfg.StallTimeout)
		defer timer.Stop()
		emit := func(s Signal) {
			select {
			case out <- s:
			case <-ctx.Done():
			}
		}
		for {
			if cfg.StallTimeout > 0 {
				timer.Reset(cfg.StallTimeout)
			}
			select {
			case <-ctx.Done():
				return
			case line, ok := <-a.Output():
				if !ok {
					emit(Signal{Kind: Exited})
					return
				}
				matched := false
				for _, re := range cfg.PromptPatterns {
					if re.Match(line) {
						emit(Signal{Kind: Prompt, Detail: string(line)})
						matched = true
						break
					}
				}
				if !matched {
					emit(Signal{Kind: Progress, Detail: string(line)})
				}
			case <-timer.C:
				emit(Signal{Kind: Stall})
			}
		}
	}()
	return out
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/agent/ -run TestWatchdogDetectsPrompt -v`
Expected: PASS.

- [ ] **Step 5: Write the failing test — a silent agent yields a Stall signal**

```go
func TestWatchdogDetectsStall(t *testing.T) {
	a, err := Start(context.Background(), Options{Command: "sh", Args: []string{"-c", "sleep 5"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = a.Kill() }()
	sigs := Watch(context.Background(), a, WatchdogConfig{StallTimeout: 500 * time.Millisecond})
	select {
	case s := <-sigs:
		if s.Kind != Stall {
			t.Fatalf("first signal = %v, want Stall", s.Kind)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watchdog did not emit Stall for a silent agent")
	}
}
```

- [ ] **Step 6: Run to verify pass**

Run: `go test ./internal/agent/ -run TestWatchdogDetectsStall -v`
Expected: PASS.

- [ ] **Step 7: Race check + commit**

Run: `go test -race ./internal/agent/`
Expected: PASS, no races.

```bash
git add internal/agent/watchdog.go internal/agent/watchdog_test.go
git commit -m "feat(agent): never-block watchdog (prompt/stall/exit signals)"
```

### Phase 1 checkpoint

- [ ] `go build ./... && go test -race ./internal/agent/ && golangci-lint run ./internal/agent/` all green.
- [ ] The control invariant is demonstrable: a prompting agent is detected before it can hang the caller.

---

## Phase 2 — User store (single XDG SQLite DB)

**Deliverable:** `internal/store` — one SQLite DB at the XDG data path, holding
migrated plan-DAG tables, `projects` (with an `identifiers` table for
accumulated fingerprints), `project_config`, `spend`, `process_tracking`, and
`sessions`/`roles` history. Immediate-tx DSN (`plandag.DSN` semantics), backup
routine.

**Key files:** `internal/store/store.go`, `internal/store/schema/*.up.sql`,
`internal/store/projects.go` (fingerprint accumulation + lookup),
`internal/store/config.go`, `internal/store/backup.go`, tests each.

**Interfaces (produced):** `store.Open(ctx, xdgPath) (*Store, error)`;
`Store.ResolveProject(fingerprints []Fingerprint) (ProjectID, bool)`;
`Store.AddProjectIdentifiers(id, []Fingerprint)`; plan-DAG methods ported from
`internal/plandag` (`CreatePlan`/`CreateTask`/`Ready`/`ClaimNextReady` with the
`RowsAffected` fix/`_txlock=immediate` DSN carried over); `Store.Backup(dir)`.

**Consumes:** `internal/xdg` for the path.

**Notes for step-expansion:** port the plandag schema + the PR-#63 safety
fixes (immediate-tx DSN, checked `RowsAffected`, error-logged writes) verbatim;
add the `project_identifiers` table (project_id, kind, value) with a unique
index so fingerprints accumulate idempotently; carry the reaper as an in-store
reclaim query (stale-heartbeat tasks → requeue) rather than the never-built
daemon reaper.

---

## Phase 3 — Config resolution engine

**Deliverable:** `internal/vconfig` — the virtual-layer resolver implementing
spec §5a exactly.

**Key files:** `internal/vconfig/vconfig.go` (layer merge),
`internal/vconfig/flags.go` (the 3 flags), `internal/vconfig/validate.go`
(merged validation + actionable missing-field report),
`internal/vconfig/diff.go` (supervisor conflict backwards-diff + auto-remove
offer), tests each.

**Interfaces (produced):** `vconfig.ResolveUser(db, cFile, userFile) (UserConfig, error)`;
`vconfig.ResolveProjects(db, userCfg) (ProjectsConfig, error)`;
`vconfig.EffectiveProject(projectsCfg, projectID, projFile, mode) (ProjectConfig, error)`
where `mode` is `Change` (persist) or `Override` (runtime-only);
`vconfig.Validate(cfg) []MissingField`;
`vconfig.DiffConflicts(stored, incoming) []Conflict`.

**Consumes:** `internal/store` (Phase 2).

**Notes for step-expansion:** precedence USER = DB < `--config-file` <
`--user-config-file`; PROJECTS = all-DB-projects < user-config `projects:`;
`Change` merges-and-stores, `Override` merges-runtime-only; `DiffConflicts`
powers the supervisor warning + auto-remove.

---

## Phase 4 — Supervisor + discovery

**Deliverable:** `--supervisor` mode + the dumb client's discover-or-refuse
handshake, reusing `internal/ipc` (socket/named-pipe) and
`internal/runtime/flock.go` (PID lock), repurposed from "attach" to "discover".

**Key files:** `internal/supervisor/supervisor.go` (lifecycle, owns agents +
store), `internal/supervisor/discovery.go` (bind = advertise; connect = discover;
single-instance + stale reclaim), `cmd/radioactive_ralph/main.go` (cobra root
command + viper config binding: routes to `--supervisor` vs client vs `--init`),
tests. This phase also performs the kong→cobra/viper migration of the CLI
surface.

**Interfaces (produced):** `supervisor.Run(ctx, cfg) error`;
`discovery.Find(xdgRuntime) (*Client, error)` (nil+err if none);
`discovery.Acquire(xdgRuntime) (*Listener, error)` (fails if a live supervisor
holds it; reclaims a stale socket via dead-PID check).

**Consumes:** `internal/store`, `internal/vconfig`, `internal/agent`, `internal/ipc`.

**Notes for step-expansion:** the socket at the XDG runtime path IS the
advertisement; client `Find` = connect-attempt; supervisor `Acquire` = bind +
flock PID + heartbeat; stale socket (connect fails, file exists, PID dead) →
remove + take over. Client refuses to run if `Find` returns none (offer to
start `--supervisor`).

---

## Phase 5 — Providers + detection

**Deliverable:** local-only provider bindings driving the Phase-1 agent runtime;
install/first-run agent detection; the `agy` spike.

**Key files:** rework `internal/provider` onto `internal/agent` (claude/codex/
opencode runners produce an `agent.Options` + parse structured result from the
`ResultPath` fd); `internal/agentdetect/detect.go` (probe PATH, classify
supported/deprecated/remote/unknown, suggest config); `internal/provider/opencode.go`
(NEW, `opencode run --format json`); `internal/provider/agy.go` (spike — bind
only if `--print` is confirmed local-surface). Tests + cassettes.

**Interfaces (produced):** `provider.Bind(name, cfg) (agent.Options, error)`;
`provider.ParseResult(path) (Usage, Outcome, error)`;
`provider.Profile` — a **capability record** (binary, invocation, result/usage
parsing, resume, and a `NativeFanout bool` for CLIs with native subagents/
workflows/parallelism), NOT a persona;
`agentdetect.Detect() []DetectedCLI` (each with `Status` supported/deprecated/
remote/unknown + reason).

**Consumes:** `internal/agent`, `internal/vconfig`.

**Notes for step-expansion:** opencode binding = `opencode run -m <p/m>
--format json`, `--variant` effort, `--session`/`--continue`, `opencode stats`
for spend; agy = `agy --print --model ...` writing result to a file; detection
distinguishes `cursor` (editor) from `cursor-agent`, and labels gemini
(deprecated) + cursor-agent (remote) with the reason they aren't offered.

---

## Phase 6 — Plan engine + orchestration (no variants)

**Deliverable:** the heuristic markdown plan engine, and the orchestrator that
dispatches steps with plan-scoped context and verifies completion. **No variant
audit** — `internal/variant` is deleted outright (one mutating Ralph).

**Key files:**
- `internal/plan/parse.go` — goldmark parse + the stop-at-next-heading-level-≤N
  section grouping; `internal/plan/decompose.go` — heuristic past/present/future
  (heading order = group dependency; under a leaf heading, unordered list =
  parallel steps, ordered = sequential; don't descend past a heading with
  subheadings); `internal/plan/validate.go` — the plan-format validator incl. the
  list-vs-bare-paragraph disambiguation rule.
- `internal/orch/orchestrator.go` — the team-lead: pick next step(s) from the
  decomposition + DB done-state, dispatch worker(s) with the plan slice as
  context, and transition a task to `done` ONLY after verifying submitted
  evidence against done-criteria (orchestrator-verified completion).
- `internal/orch/lifecycle.go` — enforcement-prompt cadence + kill/restart on
  manual context-end; per-agent XDG decision-log write + team-lead absorption.
- `internal/a2a/` — thin adoption of `a2aproject/a2a-go` `a2a.Task`/`TaskState`/
  `Message` vocabulary over the user DB (`a2a_tasks`/`a2a_messages` tables).
- Tests for each (the decomposition heuristics are pure-Go and highly testable).

**Interfaces (produced):**
`plan.Parse(md []byte) (*Plan, error)`;
`plan.Decompose(p *Plan, done map[StepID]bool) (Pending []StepGroup, Next []Step, err error)` where a `StepGroup` carries `Parallel bool`;
`orch.DispatchNext(ctx, projectID) error`;
`orch.VerifyAndComplete(ctx, taskID, evidence) (bool, error)`.

**Consumes:** `internal/store`, `internal/supervisor`, `internal/agent`, `internal/provider` (capability flag for fan-out delegation).

**Notes for step-expansion:** decomposition is pure heuristics over the goldmark
AST — no LLM, no structured output; the plan-DB slice is each worker's scoped
context (no giant dumps); a parallel step-group may be delegated to a
fan-out-capable agent (provider capability flag) instead of N Ralph workers;
cost/progress roll up to the macro TUI; completion is never agent-asserted.

---

## Phase 7 — Read-only TUI + planning genesis

**Deliverable:** the Bubble Tea client TUI (read-only macro/meso/micro live
view) AND the planning-genesis flow that turns a prompt into a reviewed plan.

**Key files:** `internal/tui/` (model/update/view split — NOT one god file; the
PR-#63 review flagged the old 1,405-line tui.go), `tui/macro.go` (plan +
hierarchy), `tui/meso.go` (plan drill → team-lead conversation; hierarchy drill
→ squad/singular), `tui/micro.go` (one agent pane / log tail),
`tui/planreview.go` (render the genesis markdown for scroll + embedded-editor or
`$EDITOR` review); `internal/genesis/genesis.go` (agent-juxtaposition
refinement → final markdown; headless emits the doc, TUI routes it to review,
skip-planning path). Tests where logic is testable.

**Interfaces:** `tui.Run(ctx, client) error`; `genesis.Refine(ctx, input) (markdown []byte, err error)`.

**Consumes:** `internal/supervisor` (events), `internal/store` (scrollback),
`internal/orch`/`internal/plan` (genesis + validation).

**Notes for step-expansion:** TUI is read-only (subscribe + replay; no direct
store writes — route through the supervisor); Lipgloss layout; genesis uses
agent juxtaposition (not question-extraction) and yields a validated markdown
plan the user can accept/edit/skip.

---

## Phase 8 — E2E, teardown, CI gating

**Deliverable:** the E2E harness, deletion of all dead old-model code, docs
sweep, and CI hardening.

**Key files:** `tests/e2e/` (fixtures from ~/src/reference-codebases/test-repo;
CI-feasible cassette-agent path + local real-agent path gated by env + spend
cap); DELETE `internal/plandag`, `internal/runtime` durable-daemon bits,
**`internal/variant` (deleted outright — no personas)**, the kong CLI wiring,
the committed-config-dir code, `internal/rlog` if still unused (or wire it in);
`docs/` sweep (drift + AI-trope + extraneous per review Phase-3/4);
`.github/workflows` CI-feasible E2E + branch protection (admin, via `gh api`) +
CodeQL-Go.

**Interfaces:** `tests/e2e` harness: detect → suggest config → set up temp repo
from fixtures → run Ralphs in logical order → assert.

**Notes for step-expansion:** fold in every remaining review finding
(reaper→supervisor reclaim [done Phase 2], CI-gating, dispatch test coverage,
runbook path drift, fabricated `RequireOperatorApproval` field, codex
turn_timeout, dead rlog); the real-agent E2E demonstrates the orchestrator
dispatching workers against a real markdown plan (heuristic decomposition →
plan-scoped context → orchestrator-verified completion) under a spend cap with
live CLI-health observation.

---

## Self-review notes

- **Spec coverage:** every spec section maps to a phase — control invariant +
  substrate (§1/§2, P1), storage/identity (§6/§5b, P2), config §5a (P3),
  supervisor + discovery §4/§5c (P4), providers + capability flag §9 (P5),
  no-variants + orchestration + plan engine §10/§11 + A2A §12 (P6), TUI §7 +
  planning genesis §11 (P7), testing strategy + teardown §13 (P8). Watchdog
  resource-threshold (§8) lands in P1 (signal kind) + P5 (config-driven
  thresholds).
- **Just-in-time step expansion (P2–P8)** is a recorded strategy decision, not a
  placeholder: Phase 1 establishes the types/patterns the later phases depend
  on, so authoring their micro-steps now would speculate against unwritten code.
  Each phase's first action is to expand its own step-level TDD detail against
  the then-current tree.
- **Type consistency:** `agent.Options`/`agent.Agent`/`Signal` (P1) are consumed
  by name in P4/P5/P6; `store.Store`/`ProjectID`/`Fingerprint` (P2) by P3/P4/P6;
  `vconfig` resolver signatures (P3) by P4/P5.
