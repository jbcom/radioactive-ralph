// Package agentdetect probes the local PATH for known agent CLIs and
// classifies each as Supported, Deprecated, RemoteDelegating, or Unknown,
// per spec docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md
// §9 ("Providers (local-only)"): "local-only" means no cloud control
// surface in the loop — calling a hosted model API for inference is fine,
// but the agent SESSION itself must not be owned by a remote service.
//
// Detect never runs a full agent turn; it only checks exec.LookPath and,
// for CLIs it finds, captures `--version` output. This keeps detection
// side-effect-free and fast enough to run on every supervisor/client
// startup.
package agentdetect

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Status classifies one detected (or expected) agent CLI.
type Status int

// The recognized Status values.
const (
	// Supported means the CLI runs a fully local agent session — no
	// forced cloud project/dashboard/session ownership in the loop —
	// and radioactive_ralph has (or plans) a runner for it.
	Supported Status = iota

	// Deprecated means the CLI/backend has been formally retired
	// upstream; radioactive_ralph will not add or keep a runner for it.
	Deprecated

	// RemoteDelegating means the CLI hands the actual agent session off
	// to a remote/cloud-hosted control surface — it fails the
	// local-only bar even though it runs as a local binary.
	RemoteDelegating

	// Unknown means detection found the binary but could not confirm
	// which bucket it belongs in (e.g. its local-vs-cloud behavior is
	// unconfirmed), or the binary is not an agent CLI at all (e.g. an
	// editor that happens to share a name with an agent CLI).
	Unknown
)

// String renders the status name for logs/errors.
func (s Status) String() string {
	switch s {
	case Supported:
		return "supported"
	case Deprecated:
		return "deprecated"
	case RemoteDelegating:
		return "remote-delegating"
	case Unknown:
		return "unknown"
	default:
		return "invalid"
	}
}

// DetectedCLI is one probed agent CLI candidate.
type DetectedCLI struct {
	// Name is the candidate's canonical name (the PATH lookup key), e.g.
	// "claude", "codex", "opencode", "gemini", "cursor-agent", "cursor",
	// "agy".
	Name string

	// Path is the resolved executable path, empty if not found on PATH.
	Path string

	// Version is the captured `--version` output (trimmed), empty if
	// the binary was not found or --version failed/produced nothing.
	Version string

	// Status classifies this CLI. See the Status constants.
	Status Status

	// Reason explains the Status, especially for Deprecated,
	// RemoteDelegating, and Unknown, where the classification is not
	// self-evident from the name alone.
	Reason string
}

// candidate is one entry in the fixed probe list Detect walks. versionArgs
// defaults to []string{"--version"} when nil.
type candidate struct {
	name        string
	status      Status
	reason      string
	versionArgs []string
}

// candidates is the fixed, documented probe list. Each entry's
// status/reason is a standing classification — not re-derived from
// `--help` at detect time — because that evidence was gathered once
// (2026-07-16, see radioactive-ralph Phase 5 provider rework) against the
// actual installed CLIs and is expected to change only when a CLI's
// upstream behavior changes, not on every Detect() call.
var candidates = []candidate{
	{
		name:   "claude",
		status: Supported,
		reason: "claude -p runs a fully local agent session; radioactive_ralph ships a runner (internal/provider.ClaudeRunner)",
	},
	{
		name:   "codex",
		status: Supported,
		reason: "codex exec runs a fully local agent session; radioactive_ralph ships a runner (internal/provider.CodexRunner)",
	},
	{
		name:   "opencode",
		status: Supported,
		reason: "opencode run --format json runs a fully local agent session bound via its local `run` path; radioactive_ralph ships a runner (internal/provider.OpencodeRunner)",
	},
	{
		name:   "gemini",
		status: Deprecated,
		reason: "CLI deprecated 2026-06-18, backend 410 Gone",
	},
	{
		name:   "cursor-agent",
		status: RemoteDelegating,
		reason: "delegates the agent session to Cursor's cloud",
	},
	{
		name:   "cursor",
		status: Unknown,
		reason: "editor, not an agent CLI",
	},
	{
		name:   "agy",
		status: Unknown,
		reason: "print-path local-surface unconfirmed; needs auth/project — `agy --print` was observed driving a cloud-backed Cloud Code/Antigravity conversation against cloudcode-pa.googleapis.com and failing with \"invalid project ID\" even with a model pinned, so it cannot be confirmed local-only; no runner is registered",
	},
}

// defaultVersionTimeout bounds each `--version` invocation so a hung or
// misbehaving CLI cannot stall Detect.
const defaultVersionTimeout = 5 * time.Second

// lookPath and runVersion are var-indirected so tests can substitute fakes
// without touching PATH or spawning real binaries.
var (
	lookPath   = exec.LookPath
	runVersion = defaultRunVersion
)

func defaultRunVersion(path string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultVersionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // path comes from exec.LookPath on a fixed candidate name, not untrusted input
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Detect probes PATH for every known agent CLI candidate and returns one
// DetectedCLI per candidate name, in the fixed order of the candidates
// table above. A candidate not found on PATH is still returned (Path and
// Version empty) so callers can report "not installed" distinctly from
// "installed but unsupported".
func Detect() []DetectedCLI {
	out := make([]DetectedCLI, 0, len(candidates))
	for _, c := range candidates {
		d := DetectedCLI{Name: c.name, Status: c.status, Reason: c.reason}
		path, err := lookPath(c.name)
		if err != nil {
			out = append(out, d)
			continue
		}
		d.Path = path
		args := c.versionArgs
		if len(args) == 0 {
			args = []string{"--version"}
		}
		if ver, err := runVersion(path, args); err == nil {
			d.Version = ver
		}
		out = append(out, d)
	}
	return out
}

// Suggest returns the names of every detected CLI classified Supported and
// actually found on PATH — the provider names radioactive_ralph should
// offer the user for this machine (e.g. during `radioactive_ralph init`
// provider selection).
func Suggest(detected []DetectedCLI) []string {
	names := make([]string, 0, len(detected))
	for _, d := range detected {
		if d.Status == Supported && d.Path != "" {
			names = append(names, d.Name)
		}
	}
	return names
}
