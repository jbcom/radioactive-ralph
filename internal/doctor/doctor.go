// Package doctor runs radioactive-ralph's environment health checks.
//
// `ralph doctor` iterates every check, prints the outcome, and exits 0
// if every hard-required check passed (soft warnings don't fail).
// This is the first thing operators run when something's off; output
// is optimised for remediation (each failure carries a one-line
// suggested fix) rather than diagnosis depth.
//
// Checks are ordered from "fundamental prerequisites" (git, claude)
// down to "nice-to-have" (specific multiplexer available). Any hard
// failure short-circuits subsequent dependent checks — no point probing
// the claude version if `claude` isn't on PATH.
package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Severity classifies a check outcome. Hard failures (FAIL) cause
// `ralph doctor` to exit non-zero. Soft failures (WARN) are printed
// but don't gate execution.
type Severity int

const (
	// OK means the check passed.
	OK Severity = iota
	// WARN means an issue was detected but is non-fatal (e.g. tmux
	// missing, we'll fall through to screen or setsid).
	WARN
	// FAIL means a hard prerequisite failed and the supervisor cannot run.
	FAIL
)

// String returns a human-friendly severity label.
func (s Severity) String() string {
	switch s {
	case OK:
		return "OK"
	case WARN:
		return "WARN"
	case FAIL:
		return "FAIL"
	}
	return "UNKNOWN"
}

// Check is one diagnostic step's output.
type Check struct {
	Name      string   // short label shown in the report ("git version")
	Severity  Severity // OK | WARN | FAIL
	Detail    string   // one-line status (e.g. "git 2.42.0 detected")
	Remediate string   // one-line suggested fix if not OK
}

// Report aggregates check outcomes plus a summary.
type Report struct {
	Checks    []Check
	OKCount   int
	WarnCount int
	FailCount int
}

// Passed reports whether the overall doctor run succeeded (zero FAILs).
func (r Report) Passed() bool {
	return r.FailCount == 0
}

// RunOptions controls the checks. Zero value runs all checks.
type RunOptions struct {
	// MinClaudeVersion is the pinned minimum Claude Code version in
	// semver (e.g. "2.1.89"). Empty means "don't pin."
	MinClaudeVersion string

	// MinGitVersion is the pinned minimum git version (e.g. "2.5.0").
	// Empty means "don't pin."
	MinGitVersion string

	// runCommand is swappable for tests. Exposed via lowercase so
	// callers must use WithRunner option.
	runCommand func(ctx context.Context, name string, args ...string) (string, error)
}

// Option configures Run.
type Option func(*RunOptions)

// WithRunner lets tests override exec.CommandContext behaviour. The
// runner receives the command name + args, returns stdout or error.
func WithRunner(fn func(ctx context.Context, name string, args ...string) (string, error)) Option {
	return func(o *RunOptions) { o.runCommand = fn }
}

// WithMinClaudeVersion pins the minimum claude CLI version expected.
func WithMinClaudeVersion(v string) Option {
	return func(o *RunOptions) { o.MinClaudeVersion = v }
}

// WithMinGitVersion pins the minimum git version expected.
func WithMinGitVersion(v string) Option {
	return func(o *RunOptions) { o.MinGitVersion = v }
}

// Run executes every check and returns a consolidated report.
// ctx is used to bound each subprocess invocation (default 5s each).
func Run(ctx context.Context, opts ...Option) Report {
	cfg := RunOptions{
		MinGitVersion: "2.5.0",
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.runCommand == nil {
		cfg.runCommand = realRunner
	}

	checks := make([]Check, 0, 5)
	checks = append(checks, checkGitVersion(ctx, cfg))
	checks = append(checks, checkClaudeVersion(ctx, cfg))
	checks = append(checks, checkGhVersion(ctx, cfg))
	checks = append(checks, checkGhAuth(ctx, cfg))
	checks = append(checks, checkMultiplexers(ctx, cfg))

	r := Report{Checks: checks}
	for _, c := range checks {
		switch c.Severity {
		case OK:
			r.OKCount++
		case WARN:
			r.WarnCount++
		case FAIL:
			r.FailCount++
		}
	}
	return r
}

// WriteText writes a human-friendly report to w. Intended for the CLI
// `ralph doctor` subcommand.
func (r Report) WriteText(w io.Writer) {
	for _, c := range r.Checks {
		_, _ = fmt.Fprintf(w, "  [%s] %s — %s\n", c.Severity, c.Name, c.Detail)
		if c.Severity != OK && c.Remediate != "" {
			_, _ = fmt.Fprintf(w, "           → %s\n", c.Remediate)
		}
	}
	_, _ = fmt.Fprintf(w, "\n%d OK, %d WARN, %d FAIL\n", r.OKCount, r.WarnCount, r.FailCount)
	if r.Passed() {
		_, _ = fmt.Fprintln(w, "Ralph's ready to run here.")
	} else {
		_, _ = fmt.Fprintln(w, "Resolve the FAIL items above before `ralph run`.")
	}
}

// ---- individual checks -------------------------------------------------

func checkGitVersion(ctx context.Context, cfg RunOptions) Check {
	out, err := withTimeout(ctx, 5*time.Second, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, "git", "--version")
	})
	if err != nil {
		return Check{
			Name:      "git",
			Severity:  FAIL,
			Detail:    "git not found on PATH",
			Remediate: "install git ≥ " + cfg.MinGitVersion + " (e.g. `brew install git` or your distro's package manager)",
		}
	}
	ver := parseVersion(out, "git version")
	if cfg.MinGitVersion != "" && !versionAtLeast(ver, cfg.MinGitVersion) {
		return Check{
			Name:      "git",
			Severity:  FAIL,
			Detail:    fmt.Sprintf("git %s (require ≥ %s for worktree support)", ver, cfg.MinGitVersion),
			Remediate: "upgrade git; worktree support requires ≥ 2.5.0",
		}
	}
	return Check{
		Name:     "git",
		Severity: OK,
		Detail:   "git " + ver,
	}
}

func checkClaudeVersion(ctx context.Context, cfg RunOptions) Check {
	out, err := withTimeout(ctx, 5*time.Second, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, "claude", "--version")
	})
	if err != nil {
		return Check{
			Name:      "claude",
			Severity:  FAIL,
			Detail:    "claude CLI not found on PATH",
			Remediate: "install Claude Code: `npm install -g @anthropic-ai/claude-code`",
		}
	}
	ver := parseVersion(out, "")
	if cfg.MinClaudeVersion != "" && !versionAtLeast(ver, cfg.MinClaudeVersion) {
		return Check{
			Name:      "claude",
			Severity:  WARN,
			Detail:    fmt.Sprintf("claude %s (newer recommended: ≥ %s)", ver, cfg.MinClaudeVersion),
			Remediate: "upgrade Claude Code: `npm update -g @anthropic-ai/claude-code`",
		}
	}
	return Check{
		Name:     "claude",
		Severity: OK,
		Detail:   "claude " + ver,
	}
}

func checkGhVersion(ctx context.Context, cfg RunOptions) Check {
	out, err := withTimeout(ctx, 5*time.Second, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, "gh", "--version")
	})
	if err != nil {
		return Check{
			Name:      "gh",
			Severity:  WARN,
			Detail:    "gh CLI not found on PATH",
			Remediate: "install GitHub CLI for PR/forge operations: `brew install gh` or https://cli.github.com",
		}
	}
	ver := parseVersion(out, "gh version")
	return Check{
		Name:     "gh",
		Severity: OK,
		Detail:   "gh " + ver,
	}
}

func checkGhAuth(ctx context.Context, cfg RunOptions) Check {
	_, err := withTimeout(ctx, 5*time.Second, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, "gh", "auth", "status")
	})
	if err != nil {
		return Check{
			Name:      "gh auth",
			Severity:  WARN,
			Detail:    "gh CLI not authenticated",
			Remediate: "run `gh auth login`",
		}
	}
	return Check{
		Name:     "gh auth",
		Severity: OK,
		Detail:   "authenticated",
	}
}

func checkMultiplexers(ctx context.Context, cfg RunOptions) Check {
	haveTmux := hasOnPath(ctx, cfg, "tmux")
	haveScreen := hasOnPath(ctx, cfg, "screen")

	switch {
	case haveTmux:
		return Check{
			Name:     "multiplexer",
			Severity: OK,
			Detail:   "tmux available (recommended)",
		}
	case haveScreen:
		return Check{
			Name:      "multiplexer",
			Severity:  WARN,
			Detail:    "tmux not found; screen will be used as fallback",
			Remediate: "install tmux for a better re-attach UX: `brew install tmux`",
		}
	default:
		// setsid is a syscall, not a binary — always available on POSIX.
		if runtime.GOOS == "windows" {
			return Check{
				Name:      "multiplexer",
				Severity:  FAIL,
				Detail:    "no multiplexer; Windows requires WSL2 for supervisor",
				Remediate: "run Ralph via WSL2 where tmux and POSIX setsid are available",
			}
		}
		return Check{
			Name:      "multiplexer",
			Severity:  WARN,
			Detail:    "neither tmux nor screen found; setsid fallback will be used (no re-attach UI)",
			Remediate: "install tmux for re-attach support: `brew install tmux`",
		}
	}
}

// ---- helpers -----------------------------------------------------------

// realRunner is the production exec.CommandContext runner.
func realRunner(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // name + args are hardcoded strings from the check list
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w (%s)", name, err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

// hasOnPath returns true if the named binary is reachable via the runner
// by invoking `<name> --version`. Using the runner (rather than
// exec.LookPath directly) keeps the test-substitution hook consistent.
func hasOnPath(ctx context.Context, cfg RunOptions, name string) bool {
	_, err := withTimeout(ctx, 2*time.Second, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, name, "--version")
	})
	return err == nil
}

// withTimeout runs fn with a per-call timeout, returning the result.
func withTimeout(parent context.Context, d time.Duration, fn func(context.Context) (string, error)) (string, error) {
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	out, err := fn(ctx)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("timeout: %w", err)
	}
	return out, err
}

// parseVersion extracts the dotted version number from a `--version`
// output. Most CLIs emit "foo version X.Y.Z ..." or "X.Y.Z"; we handle
// both shapes by stripping an optional prefix and taking the first
// dotted token.
func parseVersion(out, prefix string) string {
	trimmed := strings.TrimSpace(out)
	if prefix != "" {
		trimmed = strings.TrimPrefix(trimmed, prefix)
		trimmed = strings.TrimSpace(trimmed)
	}
	// Take the first whitespace-separated token and strip trailing
	// non-version chars (e.g. "2.42.0.1").
	for tok := range strings.FieldsSeq(trimmed) {
		if looksLikeVersion(tok) {
			return tok
		}
	}
	return trimmed
}

func looksLikeVersion(s string) bool {
	if s == "" {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	// First part must be digits.
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// versionAtLeast reports whether got >= want using simple dotted-int
// comparison. Non-numeric suffixes (e.g. "2.42.0-beta") are tolerated:
// we split on dots, compare numeric prefixes, shorter versions are
// treated as if padded with zeros.
func versionAtLeast(got, want string) bool {
	gotParts := versionParts(got)
	wantParts := versionParts(want)
	for i := range wantParts {
		var g int
		if i < len(gotParts) {
			g = gotParts[i]
		}
		w := wantParts[i]
		if g > w {
			return true
		}
		if g < w {
			return false
		}
	}
	return true
}

func versionParts(s string) []int {
	parts := make([]int, 0, 4)
	for seg := range strings.SplitSeq(s, ".") {
		n := 0
		for _, r := range seg {
			if r >= '0' && r <= '9' {
				n = n*10 + int(r-'0')
			} else {
				break
			}
		}
		parts = append(parts, n)
	}
	return parts
}
