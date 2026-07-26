package store

import (
	"context"
	"strings"
	"testing"
)

func testCalibration(alias string) ProviderCalibration {
	return ProviderCalibration{
		Alias: alias, Provider: "codex", Model: "gpt-exact", Effort: "xhigh",
		BinaryPath: "/usr/local/bin/codex", BinaryVersion: "codex 1.0.0",
		BinarySHA256: strings.Repeat("a", 64), InvocationHash: strings.Repeat("b", 64),
		InferenceDomain: "openai", ControlDomain: "local-cli",
		IndependenceDomain: "openai", Capabilities: []string{"quality.code-build-test", "runtime.local-session"},
		EvidenceJSON: `{"suite":"provider-v1","passes":3}`,
	}
}

func TestProviderCalibrationIsContentAddressedImmutableAndQueryable(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	value := testCalibration("codex-exact-xhigh")
	firstID, err := s.PutProviderCalibration(ctx, value)
	if err != nil {
		t.Fatal(err)
	}
	value.Capabilities = []string{"runtime.local-session", "quality.code-build-test"}
	secondID, err := s.PutProviderCalibration(ctx, value)
	if err != nil {
		t.Fatal(err)
	}
	if firstID != secondID || !strings.HasPrefix(firstID, "sha256:") {
		t.Fatalf("content ids = %q %q", firstID, secondID)
	}
	got, err := s.GetProviderCalibration(ctx, firstID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Alias != value.Alias || got.Model != value.Model ||
		got.IndependenceDomain != value.IndependenceDomain ||
		len(got.Capabilities) != 2 {
		t.Fatalf("round trip = %+v", got)
	}
	changed := value
	changed.Alias = "codex-exact-high"
	changed.Effort = "high"
	changedID, err := s.PutProviderCalibration(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedID == firstID {
		t.Fatal("changed calibration retained old content address")
	}
	values, err := s.ListProviderCalibrations(ctx)
	if err != nil || len(values) != 2 || values[0].Alias != "codex-exact-high" {
		t.Fatalf("list = %+v, %v", values, err)
	}
}

func TestProviderCalibrationRejectsAliasMutationAndIncompleteEvidence(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	value := testCalibration("immutable-alias")
	if _, err := s.PutProviderCalibration(ctx, value); err != nil {
		t.Fatal(err)
	}
	value.Model = "different-model"
	if _, err := s.PutProviderCalibration(ctx, value); err == nil {
		t.Fatal("same alias accepted with different calibration content")
	}
	invalid := testCalibration("invalid")
	invalid.BinarySHA256 = "not-a-hash"
	if _, err := s.PutProviderCalibration(ctx, invalid); err == nil {
		t.Fatal("invalid binary hash accepted")
	}
}
