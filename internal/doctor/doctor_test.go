package doctor

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner returns a runner that replies with predetermined output
// or errors per (name, args-joined) key.
func fakeRunner(m map[string]struct {
	out string
	err error
}) func(ctx context.Context, name string, args ...string) (string, error) {
	return func(_ context.Context, name string, args ...string) (string, error) {
		key := name
		if len(args) > 0 {
			key = name + " " + strings.Join(args, " ")
		}
		r, ok := m[key]
		if !ok {
			return "", errors.New("runner: no stub for " + key)
		}
		return r.out, r.err
	}
}

func TestSeverityString(t *testing.T) {
	if OK.String() != "OK" || WARN.String() != "WARN" || FAIL.String() != "FAIL" {
		t.Error("Severity.String wrong")
	}
	if Severity(99).String() != "UNKNOWN" {
		t.Error("default case should be UNKNOWN")
	}
}

func TestRunAllGreen(t *testing.T) {
	runner := fakeRunner(map[string]struct {
		out string
		err error
	}{
		"git --version":    {out: "git version 2.42.0"},
		"claude --version": {out: "2.1.89 (Claude Code)"},
		"gh --version":     {out: "gh version 2.60.1"},
		"gh auth status":   {out: "Logged in to github.com"},
		"tmux --version":   {out: "tmux 3.4"},
	})
	r := Run(context.Background(), WithRunner(runner), WithMinClaudeVersion("2.0.0"))
	if !r.Passed() {
		t.Errorf("expected pass, got %d failures", r.FailCount)
	}
	if r.WarnCount != 0 {
		t.Errorf("expected 0 warnings, got %d: %+v", r.WarnCount, r.Checks)
	}
}

func TestRunMissingGit(t *testing.T) {
	runner := fakeRunner(map[string]struct {
		out string
		err error
	}{
		"git --version":    {err: errors.New("not found")},
		"claude --version": {out: "2.1.89"},
		"gh --version":     {out: "gh version 2.60.1"},
		"gh auth status":   {out: "Logged in"},
		"tmux --version":   {out: "tmux 3.4"},
	})
	r := Run(context.Background(), WithRunner(runner))
	if r.Passed() {
		t.Error("expected failure for missing git")
	}
	if r.FailCount == 0 {
		t.Error("expected at least one FAIL")
	}
	// Assert the git check specifically flagged FAIL with remediation.
	var gitCheck *Check
	for i := range r.Checks {
		if r.Checks[i].Name == "git" {
			gitCheck = &r.Checks[i]
			break
		}
	}
	if gitCheck == nil {
		t.Fatal("no git check in report")
	}
	if gitCheck.Severity != FAIL {
		t.Errorf("git severity = %v, want FAIL", gitCheck.Severity)
	}
	if gitCheck.Remediate == "" {
		t.Error("missing git check should provide remediation")
	}
}

func TestRunMissingGhWarnsNotFails(t *testing.T) {
	runner := fakeRunner(map[string]struct {
		out string
		err error
	}{
		"git --version":    {out: "git version 2.42.0"},
		"claude --version": {out: "2.1.89"},
		"gh --version":     {err: errors.New("not found")},
		"gh auth status":   {err: errors.New("not found")},
		"tmux --version":   {out: "tmux 3.4"},
	})
	r := Run(context.Background(), WithRunner(runner))
	// gh is WARN, not FAIL — doctor should still pass overall.
	if !r.Passed() {
		t.Errorf("missing gh should WARN not FAIL, got %+v", r.Checks)
	}
	if r.WarnCount < 1 {
		t.Error("expected at least one WARN for missing gh")
	}
}

func TestRunMissingTmuxFallsThroughToScreen(t *testing.T) {
	runner := fakeRunner(map[string]struct {
		out string
		err error
	}{
		"git --version":    {out: "git version 2.42.0"},
		"claude --version": {out: "2.1.89"},
		"gh --version":     {out: "gh version 2.60.1"},
		"gh auth status":   {out: "Logged in"},
		"tmux --version":   {err: errors.New("not found")},
		"screen --version": {out: "Screen version 4.09.00"},
	})
	r := Run(context.Background(), WithRunner(runner))
	if !r.Passed() {
		t.Errorf("expected pass (screen fallback), got %d FAIL", r.FailCount)
	}
	// Multiplexer check should WARN not FAIL.
	for _, c := range r.Checks {
		if c.Name == "multiplexer" && c.Severity == FAIL {
			t.Errorf("multiplexer FAIL when screen available: %+v", c)
		}
	}
}

func TestRunClaudeVersionTooOldWarnsNotFails(t *testing.T) {
	runner := fakeRunner(map[string]struct {
		out string
		err error
	}{
		"git --version":    {out: "git version 2.42.0"},
		"claude --version": {out: "1.9.0"},
		"gh --version":     {out: "gh version 2.60.1"},
		"gh auth status":   {out: "Logged in"},
		"tmux --version":   {out: "tmux 3.4"},
	})
	r := Run(context.Background(), WithRunner(runner), WithMinClaudeVersion("2.1.89"))
	if !r.Passed() {
		t.Errorf("old claude should WARN not FAIL")
	}
	var claudeCheck *Check
	for i := range r.Checks {
		if r.Checks[i].Name == "claude" {
			claudeCheck = &r.Checks[i]
			break
		}
	}
	if claudeCheck == nil || claudeCheck.Severity != WARN {
		t.Errorf("expected claude WARN, got %+v", claudeCheck)
	}
}

func TestRunGitTooOldFails(t *testing.T) {
	runner := fakeRunner(map[string]struct {
		out string
		err error
	}{
		"git --version":    {out: "git version 1.9.0"},
		"claude --version": {out: "2.1.89"},
		"gh --version":     {out: "gh version 2.60.1"},
		"gh auth status":   {out: "Logged in"},
		"tmux --version":   {out: "tmux 3.4"},
	})
	r := Run(context.Background(), WithRunner(runner))
	if r.Passed() {
		t.Error("old git should FAIL")
	}
	var gitCheck *Check
	for i := range r.Checks {
		if r.Checks[i].Name == "git" {
			gitCheck = &r.Checks[i]
			break
		}
	}
	if gitCheck.Severity != FAIL {
		t.Errorf("expected git FAIL, got %v", gitCheck.Severity)
	}
}

func TestWriteTextReport(t *testing.T) {
	r := Report{
		Checks: []Check{
			{Name: "git", Severity: OK, Detail: "git 2.42"},
			{Name: "gh", Severity: WARN, Detail: "not found", Remediate: "install gh"},
		},
		OKCount: 1, WarnCount: 1,
	}
	var buf bytes.Buffer
	r.WriteText(&buf)
	out := buf.String()
	if !strings.Contains(out, "OK") || !strings.Contains(out, "git 2.42") {
		t.Errorf("report missing OK check: %s", out)
	}
	if !strings.Contains(out, "WARN") || !strings.Contains(out, "install gh") {
		t.Errorf("report missing WARN + remediation: %s", out)
	}
	if !strings.Contains(out, "Ralph's ready to run here.") {
		t.Errorf("expected success tagline (no FAILs): %s", out)
	}
}

func TestWriteTextFailsTagline(t *testing.T) {
	r := Report{
		Checks: []Check{
			{Name: "git", Severity: FAIL, Detail: "not found", Remediate: "install"},
		},
		FailCount: 1,
	}
	var buf bytes.Buffer
	r.WriteText(&buf)
	out := buf.String()
	if !strings.Contains(out, "Resolve the FAIL items") {
		t.Errorf("expected failure tagline: %s", out)
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		got, want string
		ok        bool
	}{
		{"2.42.0", "2.5.0", true},
		{"2.5.0", "2.5.0", true},
		{"2.4.9", "2.5.0", false},
		{"2.42.0", "3.0.0", false},
		{"2.42.0-beta", "2.5.0", true},
		{"2.42", "2.5", true},
		{"2", "2.0.0", true},
		{"1.9", "2.5", false},
	}
	for _, c := range cases {
		t.Run(c.got+"_vs_"+c.want, func(t *testing.T) {
			if got := versionAtLeast(c.got, c.want); got != c.ok {
				t.Errorf("versionAtLeast(%q, %q) = %v, want %v", c.got, c.want, got, c.ok)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		out, prefix, want string
	}{
		{"git version 2.42.0\n", "git version", "2.42.0"},
		{"2.1.89 (Claude Code)", "", "2.1.89"},
		{"gh version 2.60.1 (2025-08-15)", "gh version", "2.60.1"},
		{"no version here", "", "no version here"}, // fallback
	}
	for _, c := range cases {
		if got := parseVersion(c.out, c.prefix); got != c.want {
			t.Errorf("parseVersion(%q, %q) = %q, want %q", c.out, c.prefix, got, c.want)
		}
	}
}
