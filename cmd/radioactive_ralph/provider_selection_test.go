package main

import (
	"reflect"
	"testing"
)

func TestNormalizeIncomingProviderSelectionCanonicalizesLegacyAlias(t *testing.T) {
	input := map[string]any{
		providerConfigKey: " codex ",
		"model":           "gpt",
	}
	got, found, err := normalizeIncomingProviderSelection(input)
	if err != nil {
		t.Fatalf("normalizeIncomingProviderSelection: %v", err)
	}
	if !found {
		t.Fatal("selection found = false, want true")
	}
	if _, exists := got[providerConfigKey]; exists {
		t.Errorf("normalized config retained legacy key: %+v", got)
	}
	if !reflect.DeepEqual(got[providersConfigKey], []string{"codex"}) {
		t.Errorf("providers = %#v, want [codex]", got[providersConfigKey])
	}
	if input[providerConfigKey] != " codex " {
		t.Errorf("input was mutated: %+v", input)
	}
}

func TestNormalizeIncomingProviderSelectionRejectsBothKeys(t *testing.T) {
	_, _, err := normalizeIncomingProviderSelection(map[string]any{
		providerConfigKey:  "codex",
		providersConfigKey: []string{"claude"},
	})
	if err == nil {
		t.Fatal("both provider keys: want error")
	}
}

func TestNormalizeIncomingProviderSelectionRejectsInvalidPoolBeforePersistence(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
	}{
		{name: "empty", values: map[string]any{providersConfigKey: []string{}}},
		{name: "duplicate", values: map[string]any{providersConfigKey: []string{"claude", "claude"}}},
		{name: "unknown", values: map[string]any{providersConfigKey: []string{"claude", "not-a-provider"}}},
		{name: "wrong type", values: map[string]any{providersConfigKey: "claude"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := normalizeIncomingProviderSelection(tt.values); err == nil {
				t.Fatal("want validation error")
			}
		})
	}
}
