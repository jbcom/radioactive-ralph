// Package doctor runs radioactive-ralph's environment health checks.
//
// `radioactive_ralph doctor` iterates every check, prints the outcome, and exits 0
// if every hard-required check passed (soft warnings don't fail).
// This is the first thing operators run when something's off; output
// is optimised for remediation (each failure carries a one-line
// suggested fix) rather than diagnosis depth.
//
// Checks are ordered from "fundamental prerequisites" (git, provider CLIs)
// down to "nice-to-have" (service-mode ergonomics). Hard prerequisites stay
// hard failures; optional providers and auth gaps surface as warnings so the
// operator can still use the providers that are installed and logged in.
package doctor

import (
	"context"
	"fmt"
	"io"
)

// Severity classifies a check outcome. Hard failures (FAIL) cause
// `radioactive_ralph doctor` to exit non-zero. Soft failures (WARN) are printed
// but don't gate execution.
type Severity int

const (
	// OK means the check passed.
	OK Severity = iota
	// WARN means an issue was detected but is non-fatal.
	WARN
	// FAIL means a hard prerequisite failed and the runtime cannot run.
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

	checks := make([]Check, 0, 9)
	checks = append(checks, checkGitVersion(ctx, cfg))
	checks = append(checks, checkClaudeVersion(ctx, cfg))
	checks = append(checks, checkClaudeAuth(ctx, cfg))
	checks = append(checks, checkCodexVersion(ctx, cfg))
	checks = append(checks, checkCodexAuth(ctx, cfg))
	checks = append(checks, checkGeminiVersion(ctx, cfg))
	checks = append(checks, checkGeminiAuth(ctx, cfg))
	checks = append(checks, checkGhVersion(ctx, cfg))
	checks = append(checks, checkGhAuth(ctx, cfg))
	checks = append(checks, checkServicePlatform(ctx, cfg))

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
// `radioactive_ralph doctor` subcommand.
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
		_, _ = fmt.Fprintln(w, "Resolve the FAIL items above before `radioactive_ralph run`.")
	}
}
