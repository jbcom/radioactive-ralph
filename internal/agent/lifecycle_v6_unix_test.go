//go:build darwin || linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestObserverProbeCannotRunAfterExplicitReap(t *testing.T) {
	for iteration := range 10 {
		allowProbe := make(chan struct{})
		var rawProbeCalls atomic.Int32
		a, err := Start(context.Background(), Options{
			Command: "sh",
			Args:    []string{"-c", "sleep 300"},
			waitExitedForTest: func(
				probe func() (bool, error),
				_ <-chan struct{},
			) error {
				<-allowProbe
				_, err := probe()
				if err == nil {
					rawProbeCalls.Add(1)
				}
				return err
			},
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		go func() {
			<-a.terminal
			close(allowProbe)
		}()
		if err := a.TerminateAndWait(); err != nil {
			t.Fatalf("iteration %d TerminateAndWait: %v", iteration, err)
		}
		if got := rawProbeCalls.Load(); got != 0 {
			t.Fatalf("iteration %d post-reap raw probes = %d, want 0", iteration, got)
		}
	}
}

func TestTransientDirectTerminationFailureRetriesStableHandle(t *testing.T) {
	injectedErr := errors.New("injected transient stable-handle failure")
	for iteration := range 10 {
		var calls atomic.Int32
		a, err := Start(context.Background(), Options{
			Command: "sh",
			Args:    []string{"-c", "sleep 300"},
			terminateTreeForTest: func(process *os.Process) terminationOutcome {
				if calls.Add(1) == 1 {
					return terminationOutcome{
						terminationErr: errors.Join(ErrProcessTermination, injectedErr),
					}
				}
				return terminateProcessTree(process)
			},
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		if err := a.TerminateAndWait(); err != nil {
			t.Fatalf("iteration %d TerminateAndWait: %v", iteration, err)
		}
		if got := calls.Load(); got != 2 {
			t.Fatalf("iteration %d termination calls = %d, want 2", iteration, got)
		}
		phase, forced, _ := a.lifecycle.snapshot()
		if phase != lifecycleFinished || !forced {
			t.Fatalf(
				"iteration %d lifecycle = (%v, forced=%v), want forced finished",
				iteration,
				phase,
				forced,
			)
		}
	}
}

func TestNaturalExitInProbeSignalGapUsesFinalWaitStatus(t *testing.T) {
	for iteration := range 10 {
		release := make(chan struct{})
		a, err := Start(context.Background(), Options{
			Command:     "sh",
			Args:        []string{"-c", `read -r trigger; exit 23`},
			DisableEcho: true,
			probeExitedForTest: func(*os.Process) (bool, error) {
				return false, nil
			},
			terminateTreeForTest: func(process *os.Process) terminationOutcome {
				close(release)
				deadline := time.Now().Add(3 * time.Second)
				for {
					exited, observeErr := processAlreadyExited(process)
					if observeErr != nil {
						return terminationOutcome{terminationErr: observeErr}
					}
					if exited {
						return terminateProcessTree(process)
					}
					if time.Now().After(deadline) {
						return terminationOutcome{
							terminationErr: errors.New("child did not naturally exit"),
						}
					}
					runtime.Gosched()
				}
			},
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		go func() {
			<-release
			_ = a.pty.WriteAll(context.Background(), []byte("go\n"))
		}()
		if err := a.TerminateAndWait(); err != nil {
			t.Fatalf("iteration %d TerminateAndWait: %v", iteration, err)
		}
		exitErr := a.ExitErr()
		if exitErr == nil || !strings.Contains(exitErr.Error(), "exit status 23") {
			t.Fatalf("iteration %d ExitErr = %v, want natural status 23", iteration, exitErr)
		}
		phase, forced, waitErr := a.lifecycle.snapshot()
		if phase != lifecycleFinished || forced ||
			waitErr == nil || !strings.Contains(waitErr.Error(), "exit status 23") {
			t.Fatalf(
				"iteration %d lifecycle = (%v, forced=%v, wait=%v), want natural 23",
				iteration,
				phase,
				forced,
				waitErr,
			)
		}
	}
}

func TestSetpgrpDescendantCleanupIsSafeAndBounded(t *testing.T) {
	for iteration := range 10 {
		a, err := Start(context.Background(), Options{
			Command: "python3",
			Args: []string{"-c", `
import os, subprocess, sys, time
p = subprocess.Popen(
    [sys.executable, "-c",
     "import signal,time; signal.signal(signal.SIGHUP, signal.SIG_IGN); time.sleep(300)"],
    preexec_fn=os.setpgrp,
)
print(p.pid, flush=True)
time.sleep(300)
`},
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		childPID := readChildPID(t, a, iteration)
		waitErr := a.TerminateAndWait()
		if runtime.GOOS == "linux" {
			if waitErr != nil {
				t.Fatalf("iteration %d Linux TerminateAndWait: %v", iteration, waitErr)
			}
			if !waitForPIDGone(childPID) {
				_ = syscall.Kill(-childPID, syscall.SIGKILL)
				t.Fatalf("iteration %d regrouped descendant %d survived", iteration, childPID)
			}
			continue
		}

		if !errors.Is(waitErr, ErrProcessSessionCleanup) ||
			!errors.Is(waitErr, ErrProcessTreeCleanup) {
			_ = syscall.Kill(-childPID, syscall.SIGKILL)
			t.Fatalf(
				"iteration %d Darwin cleanup = %v, want compatible typed failure",
				iteration,
				waitErr,
			)
		}
		if err := syscall.Kill(childPID, 0); err != nil {
			t.Fatalf(
				"iteration %d Darwin unsafely targeted/reaped descendant %d: %v",
				iteration,
				childPID,
				err,
			)
		}
		_ = syscall.Kill(-childPID, syscall.SIGKILL)
	}
}

func TestGroupSignalConvergesBeforeReturning(t *testing.T) {
	for iteration := range 10 {
		var convergenceErr error
		a, err := Start(context.Background(), Options{
			Command: "sh",
			Args: []string{"-c", `
i=0
while [ "$i" -lt 32 ]; do
  sleep 300 &
  i=$((i+1))
done
printf 'ready\n'
wait
`},
			terminateTreeForTest: func(process *os.Process) terminationOutcome {
				outcome := terminateProcessTree(process)
				if outcome.terminationErr == nil {
					convergenceErr = errors.Join(
						convergenceErr,
						verifyNoLiveOriginalSessionDescendants(process),
					)
				}
				return outcome
			},
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		select {
		case line := <-a.Output():
			if strings.TrimSpace(string(line)) != "ready" {
				_ = a.TerminateAndWait()
				t.Fatalf("iteration %d first output = %q", iteration, line)
			}
		case <-time.After(3 * time.Second):
			_ = a.TerminateAndWait()
			t.Fatalf("iteration %d no ready line", iteration)
		}
		processGroup := a.PID()
		if processGroup <= 1 {
			cleanupErr := a.TerminateAndWait()
			t.Fatalf(
				"iteration %d PID = %d before termination, want live leader; cleanup: %v",
				iteration,
				processGroup,
				cleanupErr,
			)
		}
		if err := a.TerminateAndWait(); err != nil {
			t.Fatalf("iteration %d TerminateAndWait: %v", iteration, err)
		}
		if convergenceErr != nil {
			t.Fatalf("iteration %d process session convergence: %v", iteration, convergenceErr)
		}
		if got := a.PID(); got != 0 {
			t.Fatalf("iteration %d PID after convergence = %d, want 0", iteration, got)
		}
	}
}

func TestNaturalLeaderExitReclaimsOriginalGroupDescendant(t *testing.T) {
	for iteration := range 10 {
		a, err := Start(context.Background(), Options{
			Command: "sh",
			Args:    []string{"-c", `sleep 300 & printf '%s\n' "$!"; exit 0`},
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		childPID := readChildPID(t, a, iteration)
		if err := a.Wait(); err != nil {
			t.Fatalf("iteration %d Wait: %v", iteration, err)
		}
		if !waitForPIDGone(childPID) {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
			t.Fatalf(
				"iteration %d original-group descendant %d survived natural leader exit",
				iteration,
				childPID,
			)
		}
	}
}

func TestSetsidDescendantIsExplicitSessionBoundaryAndCannotWedgeWait(t *testing.T) {
	for iteration := range 10 {
		a, err := Start(context.Background(), Options{
			Command: "python3",
			Args: []string{"-c", `
import os, subprocess, sys, time
p = subprocess.Popen(
    [sys.executable, "-c",
     "import signal,time; signal.signal(signal.SIGHUP, signal.SIG_IGN); time.sleep(300)"],
    preexec_fn=os.setsid,
)
print(p.pid, flush=True)
time.sleep(300)
`},
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		childPID := readChildPID(t, a, iteration)
		started := time.Now()
		if err := a.TerminateAndWait(); err != nil {
			_ = syscall.Kill(-childPID, syscall.SIGKILL)
			t.Fatalf("iteration %d TerminateAndWait: %v", iteration, err)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			_ = syscall.Kill(-childPID, syscall.SIGKILL)
			t.Fatalf("iteration %d setsid PTY holder wedged Wait for %v", iteration, elapsed)
		}
		if err := syscall.Kill(childPID, 0); err != nil {
			t.Fatalf(
				"iteration %d setsid boundary child %d unexpectedly gone: %v",
				iteration,
				childPID,
				err,
			)
		}
		_ = syscall.Kill(-childPID, syscall.SIGKILL)
	}
}

func TestTerminalAwarePTYReadDrainsReadyBytesThenStops(t *testing.T) {
	for iteration := range 10 {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("iteration %d Pipe: %v", iteration, err)
		}
		if err := syscall.SetNonblock(int(reader.Fd()), true); err != nil {
			_ = reader.Close()
			_ = writer.Close()
			t.Fatalf("iteration %d SetNonblock: %v", iteration, err)
		}
		readStop := make(chan struct{})
		terminal := make(chan struct{})
		ptyReader := &nonblockingPTY{
			fd:       int(reader.Fd()),
			pollFD:   int32(reader.Fd()), //nolint:gosec // test fd is in poll range
			readStop: readStop,
			terminal: terminal,
		}
		want := []byte("ready-before-terminal\n")
		if _, err := writer.Write(want); err != nil {
			_ = reader.Close()
			_ = writer.Close()
			t.Fatalf("iteration %d Write: %v", iteration, err)
		}
		close(terminal)

		buffer := make([]byte, 128)
		n, err := ptyReader.Read(buffer)
		if err != nil || string(buffer[:n]) != string(want) {
			_ = reader.Close()
			_ = writer.Close()
			t.Fatalf(
				"iteration %d first Read = (%q, %v), want ready bytes",
				iteration,
				buffer[:n],
				err,
			)
		}
		started := time.Now()
		n, err = ptyReader.Read(buffer)
		if n != 0 || !errors.Is(err, io.EOF) {
			_ = reader.Close()
			_ = writer.Close()
			t.Fatalf("iteration %d second Read = (%d, %v), want EOF", iteration, n, err)
		}
		if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
			t.Fatalf("iteration %d terminal no-ready Read took %v", iteration, elapsed)
		}
		_ = reader.Close()
		_ = writer.Close()
	}
}

func readChildPID(t *testing.T, a *Agent, iteration int) int {
	t.Helper()
	select {
	case line := <-a.Output():
		var childPID int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(line)), "%d", &childPID); err != nil {
			_ = a.TerminateAndWait()
			t.Fatalf("iteration %d child PID line %q: %v", iteration, line, err)
		}
		return childPID
	case <-time.After(3 * time.Second):
		_ = a.TerminateAndWait()
		t.Fatalf("iteration %d no child PID", iteration)
		return 0
	}
}
