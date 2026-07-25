package provider

import "testing"

func TestCapabilitiesFailClosed(t *testing.T) {
	claude, err := ResolveShippedBinding("claude")
	if err != nil {
		t.Fatal(err)
	}
	if !claude.SupportsRequirements([]string{CapabilityLocalAgent, CapabilityNativeFanout}) {
		t.Fatal("claude should satisfy shipped local-agent and native-fanout capabilities")
	}
	codex, err := ResolveShippedBinding("codex")
	if err != nil {
		t.Fatal(err)
	}
	if codex.SupportsRequirements([]string{CapabilityNativeFanout}) {
		t.Fatal("codex must not claim an unverified native-fanout capability")
	}
	if claude.SupportsRequirements([]string{"invented-capability"}) {
		t.Fatal("unknown capabilities must fail closed")
	}
}

func TestResolveShippedBindingRejectsUnknownProvider(t *testing.T) {
	if _, err := ResolveShippedBinding("unknown"); err == nil {
		t.Fatal("unknown provider accepted")
	}
}
