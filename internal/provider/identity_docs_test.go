package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readIdentityDoc loads the design page these tests anchor.
func readIdentityDoc(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "design", "exact-provider-identity.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// No .gitattributes, so a Windows checkout may deliver CRLF.
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

// TestDocumentedSubstitutionIsReal executes the exact example the design doc
// uses to justify strict binding.
//
// The doc claims a codex binding configured only with SonnetModel="gpt-5"
// answers an OPUS request with gpt-5 and no error. If that ever stopped being
// true, the page would be arguing for a guard against a bug that no longer
// exists — and the guard would look like overengineering rather than a fix.
func TestDocumentedSubstitutionIsReal(t *testing.T) {
	doc := readIdentityDoc(t)
	if !strings.Contains(doc, `SonnetModel: "gpt-5"`) {
		t.Fatal("the doc no longer shows the gpt-5 substitution example; this test " +
			"anchors that example and must be updated with it")
	}

	binding := Binding{
		Name:   "codex-pool",
		Config: BindingConfig{Type: "codex", Binary: "codex", SonnetModel: "gpt-5"},
	}
	loose, err := ResolveInvocation(binding, Request{Model: ModelOpus, Effort: "default"})
	if err != nil {
		t.Fatalf("loose resolution errored: %v", err)
	}
	if loose.Model != "gpt-5" {
		t.Fatalf("loose Model = %q, want gpt-5 — the doc's motivating example is "+
			"no longer accurate", loose.Model)
	}
}

// TestDocumentedStrictRefusalIsReal covers the other half: the doc says strict
// mode REFUSES that same request, and names the sentinel callers match on.
func TestDocumentedStrictRefusalIsReal(t *testing.T) {
	doc := readIdentityDoc(t)
	if !strings.Contains(doc, "ErrBindingCannotHonorRequest") {
		t.Fatal("the doc no longer names ErrBindingCannotHonorRequest")
	}

	binding := Binding{
		Name:   "codex-pool",
		Config: BindingConfig{Type: "codex", Binary: "codex", SonnetModel: "gpt-5"},
	}
	_, err := ResolveInvocation(binding, Request{
		Model: ModelOpus, Effort: "default", StrictBinding: true,
	})
	if err == nil {
		t.Fatal("strict binding accepted the substitution the doc says it refuses")
	}
	if !strings.Contains(err.Error(), "opus") {
		t.Errorf("err = %v, want it to name the model that could not be honored", err)
	}
}

// TestDocumentedHashPropertiesHold checks the three claims the doc makes about
// InvocationConfigHash: alias, args, model, and effort all change it, and the
// same inputs are stable.
//
// The stability half is the one worth testing. An unstable hash makes every
// calibration lookup miss, which reads as "not calibrated yet" rather than as a
// bug — the failure would be silent and permanent.
func TestDocumentedHashPropertiesHold(t *testing.T) {
	doc := readIdentityDoc(t)
	for _, claim := range []string{"alias", "args", "model", "effort"} {
		if !strings.Contains(doc, claim) {
			t.Errorf("doc no longer mentions %q as part of the fingerprint", claim)
		}
	}

	base := Binding{Name: "b", Config: BindingConfig{Type: "codex", Binary: "codex"}}
	baseline, err := InvocationConfigHash(base, ModelSonnet, "high")
	if err != nil {
		t.Fatalf("InvocationConfigHash: %v", err)
	}
	for _, tc := range []struct {
		name    string
		binding Binding
		model   Model
		effort  string
	}{
		{"alias", Binding{Name: "other", Config: base.Config}, ModelSonnet, "high"},
		{"args", Binding{Name: "b", Config: BindingConfig{
			Type: "codex", Binary: "codex", Args: []string{"--flag"},
		}}, ModelSonnet, "high"},
		{"model", base, ModelOpus, "high"},
		{"effort", base, ModelSonnet, "low"},
	} {
		got, err := InvocationConfigHash(tc.binding, tc.model, tc.effort)
		if err != nil {
			t.Fatalf("InvocationConfigHash(%s): %v", tc.name, err)
		}
		if got == baseline {
			t.Errorf("a different %s produced the SAME hash; a calibration could be "+
				"reused across different command lines", tc.name)
		}
	}

	again, err := InvocationConfigHash(base, ModelSonnet, "high")
	if err != nil {
		t.Fatalf("InvocationConfigHash: %v", err)
	}
	if again != baseline {
		t.Fatal("hash is not stable for identical inputs; every calibration lookup " +
			"would miss and read as 'not calibrated' rather than as a bug")
	}
}

// TestDocumentedLimitsAreStated keeps the "what this does not do" section from
// quietly disappearing.
//
// It is the part most likely to be trimmed as clutter, and the part that stops
// a reader assuming the record proves more than it does — notably that Ralph
// knows the argv it BUILT, not that the CLI honored it.
func TestDocumentedLimitsAreStated(t *testing.T) {
	doc := readIdentityDoc(t)
	if !strings.Contains(doc, "What this does not do") {
		t.Fatal("the limits section is gone; without it the page reads as a " +
			"stronger guarantee than the code makes")
	}
	for _, limit := range []string{"StrictBinding", "calibration", "binary"} {
		if !strings.Contains(doc, limit) {
			t.Errorf("the limits section no longer covers %q", limit)
		}
	}
}
