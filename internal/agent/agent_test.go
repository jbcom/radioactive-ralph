//go:build !windows

// These tests exercise real pty allocation (creack/pty) and a POSIX shell,
// neither of which exists on native Windows — creack/pty returns
// ErrUnsupported there. The Windows boundary is asserted in
// agent_windows_test.go instead. Operators on Windows run Ralph under WSL,
// where this file's Unix build applies.
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

// TestKillReapsGrandchildProcess proves Kill() takes down the whole process
// GROUP, not just the direct child: an agent that spawns a long-lived grandchild
// must have that grandchild reaped when the agent is killed, or it orphans
// against the checkout (the never-block invariant's promise). The agent shell
// backgrounds a `sleep`, prints its PID, then waits; after Kill() the printed PID
// must no longer exist.
func TestKillReapsGrandchildProcess(t *testing.T) {
	ctx := context.Background()
	// Background a long sleep (the "grandchild"), print its pid, then block so the
	// agent (the direct child) stays alive holding the group open.
	a, err := Start(ctx, Options{Command: "sh", Args: []string{"-c", "sleep 300 & echo $!; wait"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Read the grandchild pid from the first output line.
	var gpid int
	deadline := time.After(3 * time.Second)
	for gpid == 0 {
		select {
		case line, ok := <-a.Output():
			if !ok {
				t.Fatal("agent output closed before printing the grandchild pid")
			}
			if p, perr := strconv.Atoi(strings.TrimSpace(string(line))); perr == nil && p > 1 {
				gpid = p
			}
		case <-deadline:
			t.Fatal("did not receive the grandchild pid within 3s")
		}
	}

	// Sanity: the grandchild is alive now (signal 0 = existence check).
	if err := syscall.Kill(gpid, 0); err != nil {
		t.Fatalf("grandchild pid %d not alive before kill: %v", gpid, err)
	}

	agentPID := a.PID()
	if err := a.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	// BOTH the direct child (agent) and the grandchild must be gone — poll briefly
	// for the group SIGKILL to land on the whole tree.
	gone := func(pid int) bool {
		for range 50 {
			if err := syscall.Kill(pid, 0); err != nil {
				return true // ESRCH (or EPERM after reap) — no longer signalable
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}
	if agentPID > 1 && !gone(agentPID) {
		t.Errorf("agent pid %d survived Kill()", agentPID)
	}
	if !gone(gpid) {
		_ = syscall.Kill(gpid, syscall.SIGKILL) // best-effort cleanup
		t.Fatalf("grandchild pid %d survived agent Kill() — process group was not reaped", gpid)
	}
}

func TestAgentStreamsOutputAndExits(t *testing.T) {
	ctx := context.Background()
	a, err := Start(ctx, Options{Command: "sh", Args: []string{"-c", "printf 'hello\\nworld\\n'"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	var got strings.Builder
	timeout := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-a.Output():
			if !ok {
				goto done
			}
			got.Write(line)
		case <-timeout:
			t.Fatal("timed out reading agent output")
		}
	}
done:
	if !strings.Contains(got.String(), "hello") || !strings.Contains(got.String(), "world") {
		t.Fatalf("output = %q, want hello+world", got.String())
	}
	if a.PID() <= 0 {
		t.Errorf("PID = %d, want > 0", a.PID())
	}
}

func TestAgentWriteInputReachesProcess(t *testing.T) {
	a, err := Start(context.Background(), Options{Command: "sh", Args: []string{"-c", "read -r line; printf 'got:%s\\n' \"$line\""}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = a.Kill() }()

	if err := a.WriteInput([]byte("hello-input\n")); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}

	var got strings.Builder
	timeout := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-a.Output():
			if !ok {
				goto done
			}
			got.Write(line)
		case <-timeout:
			t.Fatal("timed out reading agent output")
		}
	}
done:
	if !strings.Contains(got.String(), "got:hello-input") {
		t.Fatalf("output = %q, want to contain got:hello-input", got.String())
	}
}

func TestAgentKillTerminates(t *testing.T) {
	a, err := Start(context.Background(), Options{Command: "sh", Args: []string{"-c", "while true; do sleep 1; done"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := a.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case <-a.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not exit after Kill")
	}
}

// TestKillAfterNaturalExitIsNilError reproduces the review finding: an agent
// that finished on its own, then gets Kill()'d (e.g. during a normal
// supervisor shutdown that raced the agent completing), must not return a
// spurious "already closed" error.
func TestKillAfterNaturalExitIsNilError(t *testing.T) {
	a, err := Start(context.Background(), Options{Command: "sh", Args: []string{"-c", "printf 'done\\n'"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// drain output so the process can exit and readLoop can finish
	for line := range a.Output() {
		_ = line
	}
	<-a.Done()
	if err := a.Kill(); err != nil {
		t.Fatalf("Kill after natural exit = %v, want nil", err)
	}
	// second Kill is also a nil no-op
	if err := a.Kill(); err != nil {
		t.Fatalf("second Kill = %v, want nil", err)
	}
}

// TestKillUnblocksParkedReadLoop is the regression for the audit's
// back-pressure finding: readLoop now blocks on the output send rather than
// silently dropping lines, so a consumer that never reads must not deadlock
// the reader — Kill must unblock it. We start an agent that emits far more
// lines than the output buffer (256) and NEVER drain a.Output(); the
// readLoop parks on a full channel. Kill must return promptly and the done
// channel must close, proving no goroutine leak.
func TestKillUnblocksParkedReadLoop(t *testing.T) {
	// Emit ~1000 lines with no consumer so a.out (cap 256) fills and the
	// readLoop parks on its blocking send.
	a, err := Start(context.Background(), Options{
		Command: "sh",
		Args:    []string{"-c", "i=0; while [ $i -lt 1000 ]; do echo line$i; i=$((i+1)); done; sleep 30"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Give the emitter time to overrun the buffer and park the reader.
	time.Sleep(200 * time.Millisecond)

	if err := a.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	// The reader must exit (done closes) promptly after Kill, proving the
	// blocking send was released rather than leaking the goroutine.
	select {
	case <-a.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("readLoop did not exit within 3s of Kill — blocking send leaked the reader")
	}
}

// TestDisableEchoSuppressesStdinEcho is the regression for the second-pass
// audit's high finding: with DisableEcho set, a line written to the agent's
// stdin must NOT be reflected back on Output() — otherwise the watchdog
// pattern-matches the operator's own prompt text and kills a valid turn.
func TestDisableEchoSuppressesStdinEcho(t *testing.T) {
	// `cat` echoes stdin to stdout at the APPLICATION level, so to isolate
	// PTY-line-discipline echo we use a shell that reads a line and discards
	// it, producing a sentinel on stdout instead. If pty echo were on, the
	// input line would also appear on Output(); with it off, only the
	// sentinel appears.
	a, err := Start(context.Background(), Options{
		Command:     "sh",
		Args:        []string{"-c", "read -r line; printf 'READ_DONE\\n'"},
		DisableEcho: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = a.Kill() }()

	const marker = "do-you-want-to-approve-this"
	if err := a.WriteInput([]byte(marker + "\n")); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}

	var got strings.Builder
	deadline := time.After(3 * time.Second)
	for {
		select {
		case line, ok := <-a.Output():
			if !ok {
				goto done
			}
			got.WriteString(string(line))
			if strings.Contains(got.String(), "READ_DONE") {
				goto done
			}
		case <-deadline:
			goto done
		}
	}
done:
	if strings.Contains(got.String(), marker) {
		t.Errorf("stdin was echoed onto Output() despite DisableEcho; output=%q", got.String())
	}
	if !strings.Contains(got.String(), "READ_DONE") {
		t.Errorf("expected READ_DONE sentinel on output, got %q", got.String())
	}
}

// TestKillRacingNaturalExitDoesNotSignalReapedPID stresses the window where a
// Kill races readLoop's own reaping of a naturally-exiting child. Before the
// reapMu guard, Kill gated on a.done (closed two defers AFTER cmd.Wait())
// could call killProcessTree on a PID already reaped by readLoop — and possibly
// recycled by the kernel — sending SIGKILL to a bystander. Each iteration a
// short-lived agent exits on its own while a concurrent goroutine hammers
// Kill; run under -race, this must stay clean and every Kill must return nil.
func TestKillRacingNaturalExitDoesNotSignalReapedPID(t *testing.T) {
	for i := range 50 {
		a, err := Start(context.Background(), Options{
			Command: "sh", Args: []string{"-c", "printf 'x\\n'"},
		})
		if err != nil {
			t.Fatalf("iter %d Start: %v", i, err)
		}

		var wg sync.WaitGroup
		// Concurrent Kills racing the natural exit + reaping.
		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := a.Kill(); err != nil {
					t.Errorf("Kill during natural-exit race = %v, want nil", err)
				}
			}()
		}
		// Drain output so the process can exit and readLoop reaches its reaper.
		for line := range a.Output() {
			_ = line
		}
		wg.Wait()
		<-a.Done()
		// ExitErr must be readable and consistent after the dust settles.
		_ = a.ExitErr()
	}
}
