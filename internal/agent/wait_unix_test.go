//go:build !windows

package agent

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func requireWaitReleasedAgent(t *testing.T, a *Agent, pid int) {
	t.Helper()
	if _, err := a.ptmx.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("pty Stat error = %v, want os.ErrClosed", err)
	}
	if pid > 1 {
		if err := syscall.Kill(pid, 0); err == nil {
			t.Fatalf("process %d remains signalable after Wait", pid)
		}
	}
	phase, _, _ := a.lifecycle.snapshot()
	if phase != lifecycleFinished {
		t.Fatalf("lifecycle phase = %v, want finished", phase)
	}
}

func TestAgentWaitAbandonsOneAndManyUnreadLines(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{name: "one", code: `printf 'one\n'`},
		{name: "many", code: `i=0; while [ $i -lt 5000 ]; do echo line$i; i=$((i+1)); done`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, err := Start(context.Background(), Options{
				Command: "sh",
				Args:    []string{"-c", tc.code},
			})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			pid := a.PID()
			waitDone := make(chan error, 1)
			go func() { waitDone <- a.Wait() }()
			select {
			case err := <-waitDone:
				if err != nil {
					t.Fatalf("Wait: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Wait blocked on unread output")
			}
			for line := range a.Output() {
				t.Fatalf("Wait exposed abandoned line %q", line)
			}
			requireWaitReleasedAgent(t, a, pid)
		})
	}
}

func TestAgentWaitAbandonsLargeAndDiscardedRecords(t *testing.T) {
	tests := []struct {
		name   string
		bytes  int
		limit  int
		policy OversizeOutputPolicy
	}{
		{name: "large legal", bytes: 2 << 20, limit: 2 << 20, policy: RejectOversizeOutput},
		{name: "oversize discarded", bytes: 4 << 20, limit: 64 << 10, policy: DiscardOversizeOutput},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, err := Start(context.Background(), Options{
				Command: "python3",
				Args: []string{"-c",
					`import sys; sys.stdout.write("x" * ` + strconv.Itoa(tc.bytes) + ` + "\n"); sys.stdout.flush()`},
				MaxOutputRetentionBytes: RetentionBudgetForLineBytes(tc.limit),
				OversizeOutputPolicy:    tc.policy,
			})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			pid := a.PID()
			if err := a.Wait(); err != nil {
				t.Fatalf("Wait: %v", err)
			}
			if err := a.OutputErr(); err != nil {
				t.Fatalf("OutputErr: %v", err)
			}
			requireWaitReleasedAgent(t, a, pid)
		})
	}
}

func TestAgentWaitConcurrentSlowConsumerAbandonsOnlyUnreadLines(t *testing.T) {
	a, err := Start(context.Background(), Options{
		Command: "sh",
		Args: []string{"-c",
			`printf 'first\n'; sleep 0.1; printf 'second\n'; sleep 0.1; printf 'third\n'`},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	firstRead := make(chan struct{})
	releaseConsumer := make(chan struct{})
	consumerDone := make(chan []string, 1)
	go func() {
		var lines []string
		if line, ok := <-a.Output(); ok {
			lines = append(lines, strings.TrimSpace(string(line)))
		}
		close(firstRead)
		<-releaseConsumer
		for line := range a.Output() {
			lines = append(lines, strings.TrimSpace(string(line)))
		}
		consumerDone <- lines
	}()
	<-firstRead

	if err := a.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	close(releaseConsumer)
	lines := <-consumerDone
	if len(lines) != 1 || lines[0] != "first" {
		t.Fatalf("consumer lines = %v, want only already-delivered first line", lines)
	}
}

func TestAgentDrainOutputBeforeWaitPreservesTerminalFrame(t *testing.T) {
	a, err := Start(context.Background(), Options{
		Command: "sh",
		Args:    []string{"-c", `printf 'progress\nterminal-result\n'`},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	var got strings.Builder
	for line := range a.Output() {
		got.Write(line)
	}
	if err := a.Wait(); err != nil {
		t.Fatalf("Wait after drain: %v", err)
	}
	if got.String() != "progress\nterminal-result\n" {
		t.Fatalf("drained output = %q, want complete terminal frame", got.String())
	}
}

func TestAgentConcurrentWaitIsIdempotent(t *testing.T) {
	a, err := Start(context.Background(), Options{
		Command: "sh",
		Args:    []string{"-c", `i=0; while [ $i -lt 1000 ]; do echo line$i; i=$((i+1)); done`},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	var waiters sync.WaitGroup
	for range 8 {
		waiters.Add(1)
		go func() {
			defer waiters.Done()
			if err := a.Wait(); err != nil {
				t.Errorf("Wait: %v", err)
			}
		}()
	}
	waiters.Wait()
}

func TestPTYEOFWaitsForNaturalExitAndPreservesStatus(t *testing.T) {
	for iteration := range 10 {
		started := time.Now()
		a, err := Start(context.Background(), Options{
			Command: "python3",
			Args: []string{"-c",
				`import os,time,sys; os.close(0); os.close(1); os.close(2); time.sleep(.25); sys.exit(23)`},
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		for line := range a.Output() {
			_ = line
		}
		if elapsed := time.Since(started); elapsed < 200*time.Millisecond {
			t.Fatalf("iteration %d Output closed after %v; PTY EOF was treated as process exit",
				iteration, elapsed)
		}
		if err := a.Wait(); err != nil {
			t.Fatalf("iteration %d Wait: %v", iteration, err)
		}
		if exitErr := a.ExitErr(); exitErr == nil ||
			!strings.Contains(exitErr.Error(), "exit status 23") {
			t.Fatalf("iteration %d ExitErr = %v, want natural status 23", iteration, exitErr)
		}
		phase, forced, _ := a.lifecycle.snapshot()
		if phase != lifecycleFinished || forced {
			t.Fatalf("iteration %d lifecycle = (%v, forced=%v), want natural finished",
				iteration, phase, forced)
		}
	}
}

func TestPermanentObserverFailureTerminatesReapsAndReturnsTypedError(t *testing.T) {
	injectedErr := errors.New("injected permanent observer backend failure")
	for iteration := range 10 {
		a, err := Start(context.Background(), Options{
			Command: "sh",
			Args:    []string{"-c", "sleep 300"},
			waitExitedForTest: func(func() (bool, error), <-chan struct{}) error {
				return injectedErr
			},
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		pid := a.cmd.Process.Pid
		waitDone := make(chan error, 1)
		go func() { waitDone <- a.Wait() }()
		select {
		case waitErr := <-waitDone:
			if !errors.Is(waitErr, ErrProcessExitObservation) ||
				!errors.Is(waitErr, injectedErr) {
				t.Fatalf("iteration %d Wait = %v, want typed observer failure",
					iteration, waitErr)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("iteration %d permanent observer failure wedged Wait", iteration)
		}
		requireWaitReleasedAgent(t, a, pid)
	}
}

func TestProcessGroupFailureFallsBackToDirectKillAndReaps(t *testing.T) {
	injectedGroupErr := errors.New("injected process-group signal failure")
	for iteration := range 10 {
		a, err := Start(context.Background(), Options{
			Command: "sh",
			Args:    []string{"-c", "sleep 300"},
			terminateTreeForTest: func(process *os.Process) terminationOutcome {
				return terminateProcessTreeWithGroupSignal(
					process,
					func(groupID int, signal syscall.Signal) error {
						if signal == syscall.SIGKILL {
							return injectedGroupErr
						}
						return syscall.Kill(groupID, signal)
					},
				)
			},
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		pid := a.PID()
		waitErr := a.TerminateAndWait()
		if !errors.Is(waitErr, ErrProcessTreeCleanup) ||
			!errors.Is(waitErr, injectedGroupErr) ||
			errors.Is(waitErr, ErrProcessTermination) {
			t.Fatalf("iteration %d TerminateAndWait = %v, want cleanup-only failure",
				iteration, waitErr)
		}
		requireWaitReleasedAgent(t, a, pid)
	}
}

func TestPersistentDirectKillFailureHasBoundedHonestTerminalPath(t *testing.T) {
	injectedErr := errors.New("injected stable-handle kill failure")
	for iteration := range 10 {
		var terminationCalls atomic.Int32
		ctx, cancel := context.WithCancel(context.Background())
		a, err := Start(ctx, Options{
			Command: "sh",
			Args:    []string{"-c", "sleep 300"},
			terminateTreeForTest: func(*os.Process) terminationOutcome {
				terminationCalls.Add(1)
				return terminationOutcome{
					terminationErr: errors.Join(ErrProcessTermination, injectedErr),
				}
			},
		})
		if err != nil {
			cancel()
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		pid := a.cmd.Process.Pid
		cancel()

		waitDone := make(chan error, 1)
		go func() { waitDone <- a.Wait() }()
		select {
		case waitErr := <-waitDone:
			if !errors.Is(waitErr, ErrProcessTermination) ||
				!errors.Is(waitErr, injectedErr) {
				t.Fatalf("iteration %d Wait = %v, want explicit termination failure",
					iteration, waitErr)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("iteration %d persistent kill failure wedged Wait", iteration)
		}
		phase, forced, _ := a.lifecycle.snapshot()
		if phase != lifecycleFailed || forced {
			t.Fatalf("iteration %d lifecycle = (%v, forced=%v), want failed/unforced",
				iteration, phase, forced)
		}
		if got := terminationCalls.Load(); got != maxTerminationAttempts {
			t.Fatalf(
				"iteration %d termination attempts = %d, want bounded %d",
				iteration,
				got,
				maxTerminationAttempts,
			)
		}
		if err := syscall.Kill(pid, 0); err != nil {
			t.Fatalf("iteration %d falsely reported failed live child %d gone: %v",
				iteration, pid, err)
		}

		// The injected boundary deliberately made Agent relinquish ownership.
		// Reclaim the test fixture only after Wait has joined the stopped
		// observer, so this direct cmd.Wait cannot race Agent-owned reaping.
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil &&
			!errors.Is(err, syscall.ESRCH) &&
			!errors.Is(err, syscall.EPERM) {
			t.Fatalf("iteration %d fixture group kill: %v", iteration, err)
		}
		if err := a.cmd.Process.Kill(); err != nil &&
			!errors.Is(err, os.ErrProcessDone) {
			t.Fatalf("iteration %d fixture direct kill: %v", iteration, err)
		}
		_ = a.cmd.Wait()
	}
}
