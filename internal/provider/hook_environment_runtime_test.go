package provider

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProviderRunnersRejectPartialHookCoordinatesBeforeLaunch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script launch sentinel is Unix-only")
	}
	for _, tc := range []struct {
		name      string
		provider  string
		runner    Runner
		localPath bool
	}{
		{name: "claude", provider: "claude", runner: ClaudeRunner{}, localPath: true},
		{name: "codex", provider: "codex", runner: CodexRunner{}, localPath: true},
		{name: "opencode", provider: "opencode", runner: OpencodeRunner{}, localPath: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "launched")
			binary := writeFakeCLI(t, "partial-hook-"+tc.name+".sh",
				"#!/bin/sh\nprintf launched > "+shellSingleQuote(marker)+"\n")
			if out, err := exec.Command(binary).CombinedOutput(); err != nil {
				t.Fatalf("sentinel control run: %v\n%s", err, out)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("sentinel control did not create marker: %v", err)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatalf("reset sentinel marker: %v", err)
			}
			_, err := tc.runner.Run(context.Background(), Binding{
				Name: tc.provider, BinaryFromLocal: tc.localPath,
				Config: BindingConfig{Type: tc.provider, Binary: binary},
			}, Request{
				WorkingDir: t.TempDir(), ManagedSessionID: "session-canary",
			})
			if err == nil || !strings.Contains(err.Error(), "configured together") {
				t.Fatalf("Run error = %v, want partial-hook rejection", err)
			}
			if strings.Contains(err.Error(), "session-canary") {
				t.Fatalf("Run error echoed coordinate: %v", err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("provider launched despite partial coordinates: %v", err)
			}
		})
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
