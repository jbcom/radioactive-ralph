package provider

import (
	"slices"
	"testing"
)

func claudeTestArgs(t *testing.T, req Request) []string {
	t.Helper()
	binding := Binding{Name: "claude", Config: BindingConfig{Type: "claude", Binary: "claude"}}
	return claudeArgs(binding, req, "session-1", "", "")
}

// TestClaudeDisablesChrome pins the flag. Ralph drives claude
// head-authoritatively under its own pty, so the Chrome integration would
// attach a browser to a session no operator is watching. Verified present on
// claude 2.1.220: `--no-chrome  Disable Claude in Chrome integration`.
//
// Passed explicitly rather than relying on the default, because the default is
// whatever the operator's interactive config says — and a supervised
// non-interactive turn must not inherit that.
func TestClaudeDisablesChrome(t *testing.T) {
	args := claudeTestArgs(t, Request{})
	if !slices.Contains(args, "--no-chrome") {
		t.Fatalf("args = %v, want --no-chrome", args)
	}
}

// TestClaudeDoesNotBypassPermissions is the security decision, and it is a
// REFUSAL rather than an omission — recorded here so it cannot be re-added as
// an oversight.
//
// `--permission-mode bypassPermissions` was proposed to stop permission prompts
// blocking a turn. It is not needed for that, and the reason matters: the
// watchdog ALREADY kills a prompting turn and reports FailureInteractivePrompt
// (claude.go's superviseAgent path). The never-block invariant is satisfied
// without it.
//
// What bypassPermissions would actually change is the blast radius of an agent
// Ralph runs unattended against a real checkout: every permission gate the CLI
// would otherwise enforce, gone. AGENTS.md's control invariant sanctions
// "auto-resolves, DENIES, or kills-and-reclaims" — deny is explicitly listed,
// bypass is not.
//
// So: prompts stay a killable, reportable condition, not a silenced one. An
// operator who wants a permissive lane can put the flag in that binding's
// config Args, where it is a visible per-binding choice rather than a default
// nobody sees.
func TestClaudeDoesNotBypassPermissions(t *testing.T) {
	args := claudeTestArgs(t, Request{})
	for i, a := range args {
		if a == "--permission-mode" {
			next := ""
			if i+1 < len(args) {
				next = args[i+1]
			}
			t.Fatalf("args carry --permission-mode %q; permission prompts must stay "+
				"a killable, reported condition rather than a silenced one", next)
		}
		if a == "bypassPermissions" {
			t.Fatalf("args carry bypassPermissions at %d: %v", i, args)
		}
	}
}

// TestClaudeBindingArgsStillAppendLast keeps the operator escape hatch working:
// a binding's configured Args come after Ralph's, so an operator CAN opt into a
// permissive mode per binding — visibly, in that binding's config.
func TestClaudeBindingArgsStillAppendLast(t *testing.T) {
	binding := Binding{Name: "claude", Config: BindingConfig{
		Type: "claude", Binary: "claude",
		Args: []string{"--permission-mode", "bypassPermissions"},
	}}
	args := claudeArgs(binding, Request{}, "session-1", "", "")
	if len(args) < 2 {
		t.Fatalf("args = %v", args)
	}
	if args[len(args)-2] != "--permission-mode" || args[len(args)-1] != "bypassPermissions" {
		t.Fatalf("binding args must append LAST so an operator can opt in: %v", args)
	}
}

// TestClaudeKeepsItsNonInteractiveContract guards the flags the control
// invariant depends on. Losing any of these turns a supervised turn into one
// that can block or produce unparseable output.
func TestClaudeKeepsItsNonInteractiveContract(t *testing.T) {
	args := claudeTestArgs(t, Request{})
	for _, required := range []string{
		"-p", "--input-format", "--output-format", "--verbose", "--session-id",
	} {
		if !slices.Contains(args, required) {
			t.Errorf("args lost %s: %v", required, args)
		}
	}
}
