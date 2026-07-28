package provider

import (
	"context"
	"errors"
	"testing"
)

// TestDeclarativeRunnerEnforcesStrictBinding is CodeRabbit's P2 on #234.
//
// StrictBinding is on the provider-neutral Request, so a caller selecting a
// declarative runner through NewRunner could ask for strict execution and get a
// successful result anyway: declarativeTokenValues called resolveModel/
// resolveEffort directly and never went through ResolveInvocation. The pin was
// silently downgraded to a suggestion.
func TestDeclarativeRunnerEnforcesStrictBinding(t *testing.T) {
	binding := Binding{
		Name: "declarative-pool",
		// The committed-config allow-list rejects arbitrary binaries; this is
		// the local.toml override path real operators use for a custom CLI.
		BinaryFromLocal: true,
		Config: BindingConfig{
			Type: declarativePlainStdout, Binary: "true",
			SonnetModel: "some-model",
		},
	}
	_, err := DeclarativeRunner{}.Run(context.Background(), binding, Request{
		Model: ModelOpus, Effort: "high", StrictBinding: true,
		WorkingDir: t.TempDir(), UserPrompt: "hi",
	})
	if err == nil {
		t.Fatal("declarative runner accepted a strict request it cannot honor exactly; " +
			"the turn would run the sonnet fallback while the caller believes it is pinned")
	}
	if !errors.Is(err, ErrBindingCannotHonorRequest) {
		t.Fatalf("err = %v, want ErrBindingCannotHonorRequest", err)
	}
}

// TestDeclarativeRunnerReportsItsInvocation is the other half: even a loose
// request must report what ran, or provenance for every declarative provider is
// a zero value.
func TestDeclarativeRunnerReportsItsInvocation(t *testing.T) {
	binding := Binding{
		Name: "declarative-pool",
		// The committed-config allow-list rejects arbitrary binaries; this is
		// the local.toml override path real operators use for a custom CLI.
		BinaryFromLocal: true,
		Config: BindingConfig{
			Type: declarativePlainStdout, Binary: "true",
			SonnetModel: "some-model", HighEffort: "high",
		},
	}
	res, err := DeclarativeRunner{}.Run(context.Background(), binding, Request{
		Model: ModelSonnet, Effort: "high",
		WorkingDir: t.TempDir(), UserPrompt: "hi",
	})
	if err != nil {
		t.Fatalf("DeclarativeRunner.Run: %v", err)
	}
	if res.Invocation.Model != "some-model" || res.Invocation.Effort != "high" {
		t.Fatalf("Invocation = %+v, want the resolved model and effort — a zero "+
			"Invocation makes declarative provenance unusable", res.Invocation)
	}
	if res.Invocation.Alias != "declarative-pool" {
		t.Errorf("Invocation.Alias = %q, want the binding alias", res.Invocation.Alias)
	}
}
