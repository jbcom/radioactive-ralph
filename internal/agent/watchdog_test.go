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

// TestWatchNoStallWhenTimeoutNonPositive is the regression guard for the
// spurious-immediate-stall bug: Watch with StallTimeout<=0 means "no stall
// detection", so it must NOT emit a Stall on the first iteration (a timer
// created with a zero duration would otherwise fire at once and kill a healthy
// agent). The agent here produces a line then lingers; Watch must surface that
// line as Progress and never a Stall.
func TestWatchNoStallWhenTimeoutNonPositive(t *testing.T) {
	a, err := Start(context.Background(), Options{
		Command: "sh", Args: []string{"-c", "printf 'alive\\n'; sleep 2"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = a.Kill() }()

	sigs := Watch(context.Background(), a, WatchdogConfig{StallTimeout: 0})

	// Within a window that a zero-duration timer would trip almost instantly,
	// we must see the Progress line and NOT a Stall.
	deadline := time.After(1 * time.Second)
	sawProgress := false
	for {
		select {
		case sig, ok := <-sigs:
			if !ok {
				if !sawProgress {
					t.Fatal("Watch channel closed without any Progress signal")
				}
				return
			}
			switch sig.Kind {
			case Stall:
				t.Fatalf("Watch emitted a spurious Stall with StallTimeout<=0 (want none)")
			case Progress:
				sawProgress = true
			}
		case <-deadline:
			if !sawProgress {
				t.Fatal("no Progress signal within 1s")
			}
			return // no Stall seen in the window — correct
		}
	}
}
