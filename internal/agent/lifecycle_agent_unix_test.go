//go:build !windows

package agent

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func waitForPIDGone(pid int) bool {
	for range 100 {
		if err := syscall.Kill(pid, 0); err != nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestCallerCancellationReapsGrandchildProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a, err := Start(ctx, Options{
		Command: "sh",
		Args:    []string{"-c", "sleep 300 & echo $!; wait"},
	})
	if err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}

	var grandchildPID int
	select {
	case line := <-a.Output():
		grandchildPID, err = strconv.Atoi(strings.TrimSpace(string(line)))
		if err != nil || grandchildPID <= 1 {
			cancel()
			t.Fatalf("grandchild PID line = %q: %v", line, err)
		}
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("timed out waiting for grandchild PID")
	}
	cancel()
	select {
	case <-a.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("caller cancellation did not reap process group")
	}
	if !waitForPIDGone(grandchildPID) {
		_ = syscall.Kill(grandchildPID, syscall.SIGKILL)
		t.Fatalf("grandchild %d survived caller cancellation", grandchildPID)
	}
	if err := a.ExitErr(); err != nil {
		t.Fatalf("ExitErr = %v, want forced nil", err)
	}
}

func TestRepeatedConcurrentKillIsIdempotent(t *testing.T) {
	a, err := Start(context.Background(), Options{
		Command: "sh",
		Args:    []string{"-c", "sleep 300"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	var killers sync.WaitGroup
	for range 16 {
		killers.Add(1)
		go func() {
			defer killers.Done()
			if err := a.Kill(); err != nil {
				t.Errorf("Kill: %v", err)
			}
		}()
	}
	killers.Wait()
	select {
	case <-a.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("repeated Kill did not reap agent")
	}
	phase, forced, _ := a.lifecycle.snapshot()
	if phase != lifecycleFinished || !forced {
		t.Fatalf("lifecycle = (%v, forced=%v), want finished forced", phase, forced)
	}
}

func TestNaturalZeroAndNonzeroExitSemantics(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		wantError bool
	}{
		{name: "zero", status: "0"},
		{name: "nonzero", status: "29", wantError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, err := Start(context.Background(), Options{
				Command: "sh",
				Args:    []string{"-c", "exit " + tc.status},
			})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			for line := range a.Output() {
				_ = line
			}
			<-a.Done()
			if gotErr := a.ExitErr(); (gotErr != nil) != tc.wantError {
				t.Fatalf("ExitErr = %v, want error=%v", gotErr, tc.wantError)
			}
			phase, forced, _ := a.lifecycle.snapshot()
			if phase != lifecycleFinished || forced {
				t.Fatalf("lifecycle = (%v, forced=%v), want natural finished", phase, forced)
			}
		})
	}
}
