//go:build darwin || linux

package agent

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestNaturalExitReapedWhileFinalOutputAdmissionIsBlocked(t *testing.T) {
	a, err := Start(context.Background(), Options{
		Command: "sh",
		Args:    []string{"-c", `printf 'final-line\n'; exit 23`},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Deliberately leave Output unread. The unbuffered final-line admission is
	// blocked, but the kernel observer must still reap and capture status.
	select {
	case <-a.terminal:
	case <-time.After(3 * time.Second):
		t.Fatal("natural process reaping was backpressured by Output")
	}
	if err := a.ExitErr(); err == nil {
		t.Fatal("ExitErr = nil, want natural status 23")
	}
	if err := a.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestInstantNaturalExitObserverAlwaysJoins(t *testing.T) {
	for iteration := range 100 {
		a, err := Start(context.Background(), Options{
			Command: "sh",
			Args:    []string{"-c", "exit 0"},
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		waitDone := make(chan error, 1)
		go func() { waitDone <- a.Wait() }()
		select {
		case err := <-waitDone:
			if err != nil {
				t.Fatalf("iteration %d Wait: %v", iteration, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("iteration %d instant-exit observer did not join", iteration)
		}
	}
}

func TestNaturalExitIsReapedBehindBlockedOutputBeforeLateCancellation(t *testing.T) {
	for iteration := range 10 {
		ctx, cancel := context.WithCancel(context.Background())
		a, err := Start(ctx, Options{
			Command:     "sh",
			Args:        []string{"-c", `read -r trigger; printf 'final-line\n'; exit 23`},
			DisableEcho: true,
		})
		if err != nil {
			cancel()
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		pid := a.PID()

		// Hold the lifecycle claim so the kernel observer cannot reap as soon as
		// the child exits. This gives the test a deterministic, demonstrably
		// exited-but-unreaped (zombie) interval without sleep-based scheduling.
		a.lifecycle.mu.Lock()
		if err := a.WriteInput([]byte("go\n")); err != nil {
			a.lifecycle.mu.Unlock()
			cancel()
			_ = a.Kill()
			t.Fatalf("iteration %d WriteInput: %v", iteration, err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			exited, observeErr := processAlreadyExited(a.cmd.Process)
			if observeErr != nil {
				a.lifecycle.mu.Unlock()
				cancel()
				_ = a.Kill()
				t.Fatalf("iteration %d observe child %d: %v", iteration, pid, observeErr)
			}
			if exited {
				break
			}
			if time.Now().After(deadline) {
				a.lifecycle.mu.Unlock()
				cancel()
				_ = a.Kill()
				t.Fatalf("iteration %d child %d did not become exit-ready", iteration, pid)
			}
			runtime.Gosched()
		}

		// Output has no consumer, so readLoop is parked admitting final-line.
		// Cancellation is now late by construction. Releasing the lifecycle lets
		// either the observer or Kill acquire it first; both must preserve the
		// already-natural status and must not send a forced signal.
		cancel()
		killDone := make(chan error, 1)
		go func() { killDone <- a.Kill() }()
		a.lifecycle.mu.Unlock()

		select {
		case <-a.terminal:
		case <-time.After(3 * time.Second):
			t.Fatalf("iteration %d process status capture was backpressured by Output", iteration)
		}
		if err := <-killDone; err != nil {
			t.Fatalf("iteration %d late Kill: %v", iteration, err)
		}
		if err := a.ExitErr(); err == nil {
			t.Fatalf("iteration %d ExitErr = nil, want natural status 23", iteration)
		}
		phase, forced, _ := a.lifecycle.snapshot()
		if phase != lifecycleFinished || forced {
			t.Fatalf("iteration %d lifecycle = (%v, forced=%v), want natural finished", iteration, phase, forced)
		}

		// Wait's documented abandonment releases the still-blocked final line
		// and joins the output and observer goroutines.
		if err := a.Wait(); err != nil {
			t.Fatalf("iteration %d Wait: %v", iteration, err)
		}
	}
}
