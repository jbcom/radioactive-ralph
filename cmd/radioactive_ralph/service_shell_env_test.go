package main

import (
	"os"
	"testing"
)

func TestCaptureLoginShellEnvReturnsMap(t *testing.T) {
	if testing.Short() {
		t.Skip("requires a login shell")
	}
	env, err := captureLoginShellEnv()
	if err != nil {
		t.Fatalf("captureLoginShellEnv: %v", err)
	}
	if len(env) == 0 {
		t.Fatal("expected non-empty env map from login shell")
	}
	if env["HOME"] == "" {
		t.Error("HOME is missing from login shell env")
	}
	if env["PATH"] == "" {
		t.Error("PATH is missing from login shell env")
	}
}

func TestCaptureLoginShellEnvExcludesSessionVars(t *testing.T) {
	if testing.Short() {
		t.Skip("requires a login shell")
	}
	env, err := captureLoginShellEnv()
	if err != nil {
		t.Fatalf("captureLoginShellEnv: %v", err)
	}
	for key := range env {
		if shouldExcludeFromShellEnv(key) {
			t.Errorf("excluded variable %q was captured", key)
		}
	}
}

func TestShouldExcludeFromShellEnv(t *testing.T) {
	for _, key := range []string{"SHLVL", "_", "OLDPWD", "PWD", "TERM", "DISPLAY"} {
		if !shouldExcludeFromShellEnv(key) {
			t.Errorf("shouldExcludeFromShellEnv(%q) = false, want true", key)
		}
	}
	for _, key := range []string{"HOME", "PATH", "ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		if shouldExcludeFromShellEnv(key) {
			t.Errorf("shouldExcludeFromShellEnv(%q) = true, want false", key)
		}
	}
}

func TestParseEnvPairs(t *testing.T) {
	got, err := parseEnvPairs([]string{"FOO=bar", "BAZ=qux"})
	if err != nil {
		t.Fatalf("parseEnvPairs: %v", err)
	}
	if got["FOO"] != "bar" || got["BAZ"] != "qux" {
		t.Fatalf("got = %v", got)
	}
}

func TestParseEnvPairsRejectsMissingValue(t *testing.T) {
	_, err := parseEnvPairs([]string{"FOO"})
	if err == nil {
		t.Fatal("expected error for missing =")
	}
}

var _ = os.Getenv
