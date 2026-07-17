//go:build !windows

// Watchdog tests drive a real pty-backed agent (see agent_test.go's build
// note); native Windows has no pty, so they build only on Unix/WSL.
package agent

import (
	"context"
	"regexp"
	"testing"
	"time"
)

func TestWatchdogDetectsPrompt(t *testing.T) {
	a, err := Start(context.Background(), Options{
		Command: "sh",
		Args:    []string{"-c", "printf 'working...\\nDo you want to proceed? (y/n)\\n'; sleep 5"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = a.Kill() }()
	sigs := Watch(context.Background(), a, WatchdogConfig{
		StallTimeout:   3 * time.Second,
		PromptPatterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\(y/n\)|proceed\?`)},
	})
	deadline := time.After(4 * time.Second)
	for {
		select {
		case s := <-sigs:
			if s.Kind == Prompt {
				return // detected the block before the 5s sleep would hang us
			}
		case <-deadline:
			t.Fatal("watchdog did not emit Prompt for a (y/n) line")
		}
	}
}

func TestWatchdogDetectsStall(t *testing.T) {
	a, err := Start(context.Background(), Options{Command: "sh", Args: []string{"-c", "sleep 5"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = a.Kill() }()
	sigs := Watch(context.Background(), a, WatchdogConfig{StallTimeout: 500 * time.Millisecond})
	select {
	case s := <-sigs:
		if s.Kind != Stall {
			t.Fatalf("first signal = %v, want Stall", s.Kind)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watchdog did not emit Stall for a silent agent")
	}
}

// TestWatchdogGoroutineExitsAfterStall proves Stall is terminal: once emitted,
// the Watch goroutine returns and closes its channel, rather than looping and
// leaking on an abandoned channel (the consumer stops reading after the first
// Stall). A closed channel makes further receives return immediately.
func TestWatchdogGoroutineExitsAfterStall(t *testing.T) {
	a, err := Start(context.Background(), Options{Command: "sh", Args: []string{"-c", "sleep 5"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = a.Kill() }()
	sigs := Watch(context.Background(), a, WatchdogConfig{StallTimeout: 300 * time.Millisecond})

	// Consume the Stall (the only signal a real consumer reads before killing).
	select {
	case s := <-sigs:
		if s.Kind != Stall {
			t.Fatalf("first signal = %v, want Stall", s.Kind)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watchdog did not emit Stall")
	}

	// The channel must close promptly now — the goroutine returned after Stall.
	// (Without the return, it would block on the next emit and never close.)
	select {
	case _, ok := <-sigs:
		if ok {
			t.Fatal("watchdog emitted a second signal after Stall; Stall must be terminal")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog channel did not close after Stall — goroutine leaked")
	}
}
