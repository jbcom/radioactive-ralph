package provider

import (
	"slices"
	"testing"
)

func opencodeTestArgs() []string {
	binding := Binding{Name: "opencode", Config: BindingConfig{Type: "opencode", Binary: "opencode"}}
	req := Request{UserPrompt: "do the thing"}
	inv, err := ResolveInvocation(binding, req)
	if err != nil {
		panic(err)
	}
	return opencodeArgs(binding, req, inv)
}

// TestOpencodeRunsPure pins --pure, verified present on the installed opencode:
// `--pure  run without external plugins`.
//
// A supervised turn must be reproducible from the plan alone. A plugin
// installed in the operator's environment would silently change what the agent
// can do, so the same plan would behave differently on two machines for reasons
// nothing records. This is a DETERMINISM choice, which is why it lands while
// --auto does not.
func TestOpencodeRunsPure(t *testing.T) {
	if args := opencodeTestArgs(); !slices.Contains(args, "--pure") {
		t.Fatalf("args = %v, want --pure", args)
	}
}

// TestOpencodeDoesNotAutoApprove is a REFUSAL, recorded so it cannot be
// re-added as an oversight.
//
// opencode's own help calls it what it is: `--auto  auto-approve permissions
// that are not explicitly denied (dangerous!)`. It is the same class of change
// as claude's bypassPermissions, and the same reasoning applies — the watchdog
// already kills a prompting turn and reports FailureInteractivePrompt, so the
// never-block invariant does not need it. What it WOULD change is the blast
// radius of an agent running unattended against a real checkout.
//
// AGENTS.md's control invariant sanctions "auto-resolves, DENIES, or
// kills-and-reclaims". Deny is listed; auto-approve is not.
func TestOpencodeDoesNotAutoApprove(t *testing.T) {
	if args := opencodeTestArgs(); slices.Contains(args, "--auto") {
		t.Fatalf("args carry --auto (opencode labels it dangerous); permission "+
			"prompts must stay a killable, reported condition: %v", args)
	}
}

// TestOpencodeBindingArgsStillAppendLast keeps the operator escape hatch: a
// binding that genuinely wants --auto can carry it in its own config Args,
// where it is a visible per-binding choice rather than an invisible default.
func TestOpencodeBindingArgsStillAppendLast(t *testing.T) {
	binding := Binding{Name: "opencode", Config: BindingConfig{
		Type: "opencode", Binary: "opencode", Args: []string{"--auto"},
	}}
	req := Request{UserPrompt: "x"}
	inv, err := ResolveInvocation(binding, req)
	if err != nil {
		t.Fatalf("ResolveInvocation: %v", err)
	}
	args := opencodeArgs(binding, req, inv)
	if len(args) == 0 || args[len(args)-1] != "--auto" {
		t.Fatalf("binding args must append LAST so an operator can opt in: %v", args)
	}
}

// TestOpencodeKeepsItsStructuredOutputContract guards the flags the runner's
// parsing depends on.
func TestOpencodeKeepsItsStructuredOutputContract(t *testing.T) {
	args := opencodeTestArgs()
	for _, required := range []string{"run", "--format", "json"} {
		if !slices.Contains(args, required) {
			t.Errorf("args lost %s: %v", required, args)
		}
	}
}
