//go:build windows

package agent

import (
	"os/exec"
	"sync/atomic"
	"testing"
)

func TestWindowsStableHandleObservesNaturalExitBeforeLateForce(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/d", "/s", "/c", "exit 23")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := waitProcessExited(
		func() (bool, error) { return processAlreadyExited(cmd.Process) },
		make(chan struct{}),
	); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("waitProcessExited: %v", err)
	}
	exited, err := processAlreadyExited(cmd.Process)
	if err != nil {
		_ = cmd.Wait()
		t.Fatalf("processAlreadyExited: %v", err)
	}
	if !exited {
		_ = cmd.Wait()
		t.Fatal("stable Windows process handle did not report signaled exit")
	}

	lifecycle := &processLifecycle{}
	var signalCalls atomic.Int32
	claim := lifecycle.claimTermination(
		func() (bool, error) { return processAlreadyExited(cmd.Process) },
		func() terminationOutcome {
			signalCalls.Add(1)
			return terminationOutcome{terminationErr: cmd.Process.Kill()}
		},
	)
	if claim.claimed || !claim.natural || claim.observationErr != nil ||
		claim.terminationErr != nil || signalCalls.Load() != 0 {
		_ = cmd.Wait()
		t.Fatalf("late claim = %+v, signals=%d; want observed natural exit",
			claim, signalCalls.Load())
	}
	claimed, err := lifecycle.claimNatural(func() error { return nil })
	if !claimed || err != nil {
		_ = cmd.Wait()
		t.Fatalf("claimNatural = (%v, %v), want claimed", claimed, err)
	}
	lifecycle.finish(cmd.Wait())
	if lifecycle.exitError() == nil {
		t.Fatal("natural Windows exit status 23 was suppressed")
	}
}
