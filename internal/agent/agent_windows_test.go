//go:build windows

package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestStartReturnsErrWSLNotFoundWhenWslExeMissing replaces the old
// TestStartUnsupportedOnWindows, which asserted the pre-2026-08-20 ConPTY-era
// behavior (Start always fails with ErrPTYUnsupported on native Windows).
// That's no longer true: Start dispatches through wsl.exe instead (see
// pty_start_windows.go's package doc for why). What IS still deterministically
// testable without a provisioned distro is the "wsl.exe isn't even on PATH"
// failure mode, which this asserts by pointing PATH at an empty directory.
func TestStartReturnsErrWSLNotFoundWhenWslExeMissing(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)

	a, err := Start(context.Background(), Options{Command: "cmd", Args: []string{"/c", "echo hi"}})
	if a != nil {
		_ = a.Kill()
		t.Fatalf("Start returned a live agent with no wsl.exe on PATH; expected ErrWSLNotFound")
	}
	if !errors.Is(err, ErrWSLNotFound) {
		t.Fatalf("Start error = %v, want ErrWSLNotFound", err)
	}
}

// wslTestDistro returns the name of a registered WSL2 distro to dispatch
// into for these tests, or skips if none is available. It does not require
// the real "radioactive-ralph" distro (nothing provisions that in CI or on
// an arbitrary dev machine) -- any already-registered, running-capable
// distro proves the same dispatch mechanism. Docker Desktop's own
// "docker-desktop" distro is a convenient, commonly-present stand-in.
func wslTestDistro(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		t.Skip("wsl.exe not on PATH; skipping WSL dispatch integration test")
	}
	out, err := exec.Command("wsl.exe", "-l", "-q").CombinedOutput()
	if err != nil {
		t.Skipf("wsl.exe -l -q failed (%v); skipping WSL dispatch integration test", err)
	}
	// wsl.exe emits UTF-16LE even with -q; decode enough to find a name.
	text := decodeUTF16ish(out)
	for _, line := range strings.Split(text, "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			return name
		}
	}
	t.Skip("no registered WSL2 distro found; skipping WSL dispatch integration test")
	return ""
}

// decodeUTF16ish strips NUL bytes from wsl.exe's UTF-16LE console output --
// good enough to recover ASCII distro names without pulling in a full
// UTF-16 decoder for one test helper.
func decodeUTF16ish(b []byte) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c != 0 {
			out = append(out, c)
		}
	}
	return string(out)
}

// TestStartDispatchesOneShotInputThroughWSL is the real, non-mocked proof
// that agent.Start's Windows path works end to end: a one-shot input turn
// (the exact protocol internal/provider/claude.go uses) through a real WSL2
// distro must write cleanly, be observed by the child, and let the child
// exit on its own EOF -- the precise behavior ConPTY was confirmed unable to
// provide.
func TestStartDispatchesOneShotInputThroughWSL(t *testing.T) {
	distro := wslTestDistro(t)

	old := RalphWSLDistroName
	RalphWSLDistroName = distro
	t.Cleanup(func() { RalphWSLDistroName = old })

	payload := []byte("hello-from-agent-test\n")
	a, err := Start(context.Background(), Options{
		Command:      "cat",
		OneShotInput: payload,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.TerminateAndWait() })

	var got bytes.Buffer
	deadline := time.After(15 * time.Second)
collect:
	for {
		select {
		case chunk, ok := <-a.Output():
			if !ok {
				break collect
			}
			got.Write(chunk)
		case <-a.Done():
			break collect
		case <-deadline:
			t.Fatal("timed out waiting for output; child never observed EOF through WSL dispatch")
		}
	}

	if err := a.Wait(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Wait: %v", err)
	}
	if !bytes.Contains(got.Bytes(), []byte("hello-from-agent-test")) {
		t.Fatalf("child did not echo the written input; got %q", got.String())
	}
}
