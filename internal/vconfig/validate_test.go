package vconfig

import (
	"strings"
	"testing"
)

// TestValidateReportsMissingRequiredKeys verifies Validate reports every
// required key that is absent or empty, and none that are present with a
// non-empty value.
func TestValidateReportsMissingRequiredKeys(t *testing.T) {
	cfg := ProjectConfig{Values: map[string]any{
		"provider":  "claude",
		"model":     "", // present but empty -> still missing
		"api_token": nil,
	}}
	required := []string{"provider", "model", "api_token", "workspace"}

	missing := Validate(cfg, required)

	got := map[string]bool{}
	for _, m := range missing {
		got[m.Key] = true
		if m.Reason == "" {
			t.Errorf("MissingField %q has empty Reason", m.Key)
		}
	}

	if got["provider"] {
		t.Error("provider should not be reported missing (has a non-empty value)")
	}
	for _, want := range []string{"model", "api_token", "workspace"} {
		if !got[want] {
			t.Errorf("expected %q to be reported missing; missing=%+v", want, missing)
		}
	}
	if len(missing) != 3 {
		t.Errorf("len(missing) = %d, want 3: %+v", len(missing), missing)
	}
}

// TestValidateNoneMissing verifies an empty result (not nil-vs-empty
// sensitive) when every required key is set.
func TestValidateNoneMissing(t *testing.T) {
	cfg := ProjectConfig{Values: map[string]any{"provider": "claude"}}
	missing := Validate(cfg, []string{"provider"})
	if len(missing) != 0 {
		t.Errorf("Validate = %+v, want empty", missing)
	}
}

// TestFormatMissing verifies the formatted message is actionable and empty
// when there's nothing missing.
func TestFormatMissing(t *testing.T) {
	if got := FormatMissing(nil); got != "" {
		t.Errorf("FormatMissing(nil) = %q, want empty string", got)
	}

	missing := []MissingField{{Key: "provider", Reason: "required config key not set"}}
	msg := FormatMissing(missing)
	if msg == "" {
		t.Fatal("FormatMissing returned empty for non-empty input")
	}
	for _, want := range []string{"provider", "required config key not set", "--config-file", "--user-config-file", "init wizard"} {
		if !strings.Contains(msg, want) {
			t.Errorf("FormatMissing message %q missing substring %q", msg, want)
		}
	}
}
