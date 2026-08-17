//go:build !windows

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
)

func TestAdaptersInstallCustomTargetBecomesRuntimeDefault(t *testing.T) {
	t.Setenv("RALPH_STATE_DIR", t.TempDir())
	target := filepath.Join(t.TempDir(), "custom-adapters")
	cmd := newAdaptersCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"install", "--target", target})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("adapters install: %v", err)
	}
	bundle, err := adapters.CurrentBundleFromEnvironment(func(string) string { return "" })
	if err != nil {
		t.Fatalf("managed runtime did not discover CLI target: %v", err)
	}
	want, err := filepath.Abs(target)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if bundle.Target != want {
		t.Fatalf("managed runtime target = %q, want %q", bundle.Target, want)
	}
}
