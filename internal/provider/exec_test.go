package provider

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRunCommandSeparatesStderr verifies stderr noise is not folded
// into the stdout we return as AssistantOutput. Regression guard for
// the Gemini case where the CLI prints warnings to stderr that we
// were previously concatenating into the result.
func TestRunCommandSeparatesStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script — skip on windows")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// sh prints `ok` to stdout and `noise` to stderr; success path
	// should return "ok" only.
	out, err := runCommand(ctx, "", "sh", []string{"-c", `printf ok; printf noise 1>&2`})
	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	if out != "ok" {
		t.Errorf("got %q, want %q", out, "ok")
	}
}

// TestRunCommandErrorIncludesStderr verifies stderr is surfaced on
// non-zero exit. Without this, an opaque "exit 1" tells the operator
// nothing.
func TestRunCommandErrorIncludesStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script — skip on windows")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := runCommand(ctx, "", "sh", []string{"-c", `printf diag 1>&2; exit 7`})
	if err == nil {
		t.Fatal("expected error for exit 7")
	}
	if !strings.Contains(err.Error(), "diag") {
		t.Errorf("error does not include stderr content: %v", err)
	}
}
