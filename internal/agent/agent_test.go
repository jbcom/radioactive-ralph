//go:build !windows

// These tests exercise real pty allocation (creack/pty) and a POSIX shell,
// neither of which exists on native Windows — creack/pty returns
// ErrUnsupported there. The Windows boundary is asserted in
// agent_windows_test.go instead. Operators on Windows run Ralph under WSL,
// where this file's Unix build applies.
package agent

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type alwaysFailReader struct{}

func (alwaysFailReader) Read([]byte) (int, error) {
	return 0, errors.New("sensitive device failure")
}

type sizedLineReader struct {
	remaining int
	newline   bool
}

func (r *sizedLineReader) Read(p []byte) (int, error) {
	if r.remaining > 0 {
		n := min(len(p), r.remaining)
		for i := range n {
			p[i] = 'x'
		}
		r.remaining -= n
		return n, nil
	}
	if !r.newline {
		p[0] = '\n'
		r.newline = true
		return 1, nil
	}
	return 0, io.EOF
}

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
	if a.PID() <= 0 {
		t.Fatalf("PID = %d while running, want > 0", a.PID())
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
	if a.PID() != 0 {
		t.Errorf("PID = %d after reaping, want 0", a.PID())
	}
}

func TestAgentConfiguredLargeLineIsDrained(t *testing.T) {
	const payloadBytes = 2 << 20
	a, err := Start(context.Background(), Options{
		Command:                 "python3",
		Args:                    []string{"-c", `import sys; sys.stdout.write("x" * (2 << 20) + "\n"); sys.stdout.flush()`},
		MaxOutputRetentionBytes: RetentionBudgetForLineBytes(payloadBytes),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	var got []byte
	select {
	case got = <-a.Output():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out draining configured large output line")
	}
	for line := range a.Output() {
		got = append(got, line...)
	}
	if len(got) != payloadBytes+1 {
		t.Fatalf("large line length = %d, want %d payload bytes plus newline", len(got), payloadBytes+1)
	}
	if err := a.OutputErr(); err != nil {
		t.Fatalf("OutputErr = %v, want nil for legal configured line", err)
	}
	if err := a.ExitErr(); err != nil {
		t.Fatalf("ExitErr = %v, want clean child exit", err)
	}
}

func TestWriteInputRetriesMultiMiBBackpressureWithoutBusySpin(t *testing.T) {
	const payloadBytes = 2 << 20
	var blocked atomic.Int32
	a, err := Start(context.Background(), Options{
		Command: "python3",
		Args: []string{"-c", `import os,sys,time,tty
tty.setraw(0)
os.write(1,b"ready\n")
time.sleep(.25)
remaining=` + strconv.Itoa(payloadBytes) + `
received=0
while remaining:
    chunk=os.read(0,min(65536,remaining))
    if not chunk:
        break
    received+=len(chunk)
    remaining-=len(chunk)
os.write(1,("\nreceived:%d\n" % received).encode())`},
		onWriteBlockForTest: func() { blocked.Add(1) },
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case line := <-a.Output():
		if strings.TrimSpace(string(line)) != "ready" {
			_ = a.TerminateAndWait()
			t.Fatalf("first output = %q, want ready", line)
		}
	case <-time.After(3 * time.Second):
		_ = a.TerminateAndWait()
		t.Fatal("timed out waiting for raw-input child")
	}

	payload := []byte(strings.Repeat("x", payloadBytes))
	writeDone := make(chan error, 1)
	go func() { writeDone <- a.WriteInput(payload) }()

	// The child deliberately does not read for 250ms. The writer must remain
	// blocked and use the 10ms readiness poll, not spin through EAGAIN.
	time.Sleep(100 * time.Millisecond)
	select {
	case writeErr := <-writeDone:
		_ = a.TerminateAndWait()
		t.Fatalf("WriteInput returned before the child read: %v", writeErr)
	default:
	}
	if calls := blocked.Load(); calls == 0 || calls > 30 {
		_ = a.TerminateAndWait()
		t.Fatalf("write-block polls after 100ms = %d, want bounded readiness waits", calls)
	}

	select {
	case writeErr := <-writeDone:
		if writeErr != nil {
			_ = a.TerminateAndWait()
			t.Fatalf("WriteInput: %v", writeErr)
		}
	case <-time.After(10 * time.Second):
		_ = a.TerminateAndWait()
		t.Fatal("WriteInput did not deliver the complete multi-MiB payload")
	}

	var got strings.Builder
	for line := range a.Output() {
		got.Write(line)
	}
	if err := a.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !strings.Contains(got.String(), "received:"+strconv.Itoa(payloadBytes)) {
		t.Fatalf("child output = %q, want complete byte count %d", got.String(), payloadBytes)
	}
}

func TestAgentOverLimitKillsReapsAndSurfacesPromptly(t *testing.T) {
	const limit = 64 << 10
	a, err := Start(context.Background(), Options{
		Command: "python3",
		Args: []string{"-c",
			`import sys,time; sys.stdout.write("x" * ((64 << 10) + 1)); sys.stdout.flush(); time.sleep(300)`},
		MaxOutputRetentionBytes: RetentionBudgetForLineBytes(limit),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := a.PID()
	start := time.Now()
	select {
	case <-a.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("over-limit agent was not killed and reaped promptly")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("over-limit agent took %s to terminate", elapsed)
	}
	if !errors.Is(a.OutputErr(), ErrOutputLineTooLong) {
		t.Fatalf("OutputErr = %v, want ErrOutputLineTooLong", a.OutputErr())
	}
	if err := a.ExitErr(); err != nil {
		t.Fatalf("ExitErr = %v, want nil killed semantics after output-contract kill", err)
	}
	if pid > 1 {
		if err := syscall.Kill(pid, 0); err == nil {
			t.Fatalf("over-limit child pid %d still exists after Done closed", pid)
		}
	}
}

func TestAgentDiscardPolicyDrainsEndlessLineUntilCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a, err := Start(ctx, Options{
		Command: "python3",
		Args: []string{"-u", "-c",
			`import sys; chunk=b"x"*(64<<10)
while True:
 sys.stdout.buffer.write(chunk)
 sys.stdout.buffer.flush()`},
		MaxOutputRetentionBytes: RetentionBudgetForLineBytes(4 << 10),
		OversizeOutputPolicy:    DiscardOversizeOutput,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-a.Activity():
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not drain bytes from endless output line")
	}
	cancel()
	select {
	case <-a.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("caller cancellation did not kill and reap endless-output agent")
	}
	if err := a.OutputErr(); err != nil {
		t.Fatalf("OutputErr = %v, want nil for discard-mode cancellation", err)
	}
	if err := a.ExitErr(); err != nil {
		t.Fatalf("ExitErr = %v, want nil forced-kill semantics after caller cancellation", err)
	}
	for line := range a.Output() {
		t.Fatalf("discarded endless line unexpectedly reached Output: %d bytes", len(line))
	}
}

func TestAgentObservedOutputCeilingKillsReapsDiscardedEndlessLine(t *testing.T) {
	const limit = 96 << 10
	a, err := Start(context.Background(), Options{
		Command: "sh",
		Args: []string{"-c", `sleep 300 & printf '%s\n' "$!"; exec python3 -u -c '
import sys
chunk=b"x"*(64<<10)
while True:
 sys.stdout.buffer.write(chunk)
 sys.stdout.buffer.flush()
'`},
		MaxOutputRetentionBytes: RetentionBudgetForLineBytes(4 << 10),
		MaxObservedOutputBytes:  limit,
		OversizeOutputPolicy:    DiscardOversizeOutput,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	agentPID := a.PID()

	var descendantPID int
	select {
	case line := <-a.Output():
		descendantPID, err = strconv.Atoi(strings.TrimSpace(string(line)))
		if err != nil || descendantPID <= 1 {
			_ = a.TerminateAndWait()
			t.Fatalf("descendant PID line = %q: %v", line, err)
		}
	case <-time.After(3 * time.Second):
		_ = a.TerminateAndWait()
		t.Fatal("timed out waiting for descendant PID")
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- a.Wait() }()
	select {
	case waitErr := <-waitDone:
		if !errors.Is(waitErr, ErrObservedOutputTooLarge) {
			t.Fatalf("Wait = %v, want ErrObservedOutputTooLarge", waitErr)
		}
	case <-time.After(3 * time.Second):
		_ = a.TerminateAndWait()
		t.Fatal("observed-output ceiling did not terminate and join endless output")
	}
	if a.OutputErr() != ErrObservedOutputTooLarge {
		t.Fatalf("OutputErr = %v, want static ErrObservedOutputTooLarge", a.OutputErr())
	}
	for line := range a.Output() {
		t.Fatalf("discarded endless line unexpectedly reached Output: %d bytes", len(line))
	}
	requireWaitReleasedAgent(t, a, agentPID)
	if !waitForPIDGone(descendantPID) {
		_ = syscall.Kill(descendantPID, syscall.SIGKILL)
		t.Fatalf("descendant pid %d survived observed-output convergence", descendantPID)
	}
}

func TestAgentObservedOutputErrorSurvivesNaturalExitRace(t *testing.T) {
	const limit = 4 << 10
	for iteration := range 10 {
		a, err := Start(context.Background(), Options{
			Command: "python3",
			Args: []string{"-c",
				`import os; os.write(1, b"x" * (` + strconv.Itoa(limit) + ` + 1)); raise SystemExit(23)`},
			MaxOutputRetentionBytes: RetentionBudgetForLineBytes(8 << 10),
			MaxObservedOutputBytes:  limit,
		})
		if err != nil {
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		pid := a.PID()
		waitErr := a.Wait()
		if !errors.Is(waitErr, ErrObservedOutputTooLarge) {
			t.Fatalf(
				"iteration %d Wait = %v, want ErrObservedOutputTooLarge",
				iteration,
				waitErr,
			)
		}
		if a.OutputErr() != ErrObservedOutputTooLarge {
			t.Fatalf(
				"iteration %d OutputErr = %v, want static ErrObservedOutputTooLarge",
				iteration,
				a.OutputErr(),
			)
		}
		requireWaitReleasedAgent(t, a, pid)
	}
}

func TestAgentObservedOutputErrorSurvivesConcurrentCancellation(t *testing.T) {
	const limit = 4 << 10
	ctx, cancel := context.WithCancel(context.Background())
	terminationStarted := make(chan struct{})
	releaseTermination := make(chan struct{})
	var startOnce sync.Once
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseTermination) }) }

	a, err := Start(ctx, Options{
		Command: "python3",
		Args: []string{"-c",
			`import os,time; os.write(1, b"x" * (` + strconv.Itoa(limit) + ` + 1)); time.sleep(300)`},
		MaxOutputRetentionBytes: RetentionBudgetForLineBytes(8 << 10),
		MaxObservedOutputBytes:  limit,
		terminateTreeForTest: func(process *os.Process) terminationOutcome {
			startOnce.Do(func() { close(terminationStarted) })
			<-releaseTermination
			return terminateProcessTree(process)
		},
	})
	if err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		release()
		cancel()
		_ = a.TerminateAndWait()
	}()

	select {
	case <-terminationStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("observed-output failure did not begin active termination")
	}
	cancel()
	release()

	waitErr := a.Wait()
	if !errors.Is(waitErr, ErrObservedOutputTooLarge) {
		t.Fatalf("Wait = %v, want ErrObservedOutputTooLarge", waitErr)
	}
	if a.OutputErr() != ErrObservedOutputTooLarge {
		t.Fatalf("OutputErr = %v, want static ErrObservedOutputTooLarge", a.OutputErr())
	}
}

func TestCallerCancellationMarksAgentExitAsForced(t *testing.T) {
	for iteration := range 10 {
		ctx, cancel := context.WithCancel(context.Background())
		a, err := Start(ctx, Options{
			Command: "sh",
			Args:    []string{"-c", "sleep 300"},
		})
		if err != nil {
			cancel()
			t.Fatalf("iteration %d Start: %v", iteration, err)
		}
		cancel()
		select {
		case <-a.Done():
		case <-time.After(3 * time.Second):
			t.Fatalf("iteration %d caller cancellation did not reap agent", iteration)
		}
		if err := a.ExitErr(); err != nil {
			t.Fatalf("iteration %d ExitErr = %v, want nil caller-forced semantics", iteration, err)
		}
	}
}

func TestNaturalExitRemainsVisibleWhenCancellationComesAfterDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a, err := Start(ctx, Options{
		Command: "sh",
		Args:    []string{"-c", "exit 23"},
	})
	if err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	for range a.Output() {
		cancel()
	}
	<-a.Done()
	cancel()
	if err := a.ExitErr(); err == nil {
		t.Fatal("ExitErr = nil, want natural nonzero exit preserved after later cancellation")
	}
}

func TestReadOutputLineContracts(t *testing.T) {
	t.Run("exact limit and CRLF", func(t *testing.T) {
		const payload = "123456789012345"
		line, discarded, err := newOutputLineReader(
			// payload + CR exactly fills a historical scanner boundary; the
			// streaming reader must still
			// normalize the delimiter without counting CR as payload.
			strings.NewReader(payload+"\r\n"),
			nil,
		).nextLine(
			len(payload),
			RejectOversizeOutput,
		)
		if err != nil {
			t.Fatalf("readOutputLine: %v", err)
		}
		if discarded {
			t.Fatal("exact-limit line was discarded")
		}
		if got := string(line); got != payload+"\n" {
			t.Fatalf("line = %q, want normalized newline", got)
		}
	})

	t.Run("over limit", func(t *testing.T) {
		_, _, err := newOutputLineReader(
			strings.NewReader("123456789\n"),
			nil,
		).nextLine(
			8,
			RejectOversizeOutput,
		)
		if !errors.Is(err, ErrOutputLineTooLong) {
			t.Fatalf("error = %v, want ErrOutputLineTooLong", err)
		}
	})

	t.Run("discard drains and preserves next line", func(t *testing.T) {
		reader := newOutputLineReader(strings.NewReader("123456789\nok\n"), nil)
		_, discarded, err := reader.nextLine(8, DiscardOversizeOutput)
		if err != nil || !discarded {
			t.Fatalf("oversize discard = (%v, %v), want discarded without error", discarded, err)
		}
		line, discarded, err := reader.nextLine(8, DiscardOversizeOutput)
		if err != nil || discarded || string(line) != "ok\n" {
			t.Fatalf("next line = (%q, %v, %v), want preserved ok line", line, discarded, err)
		}
	})

	t.Run("read error is static", func(t *testing.T) {
		_, _, err := newOutputLineReader(
			alwaysFailReader{},
			nil,
		).nextLine(
			8,
			RejectOversizeOutput,
		)
		if !errors.Is(err, ErrOutputRead) {
			t.Fatalf("error = %v, want ErrOutputRead", err)
		}
		if strings.Contains(err.Error(), "sensitive device failure") {
			t.Fatalf("read error leaked source detail: %q", err)
		}
	})
}

func BenchmarkDiscardOversizeOutputLine(b *testing.B) {
	for _, size := range []int{8 << 20, 64 << 20} {
		b.Run(strconv.Itoa(size>>20)+"MiB", func(b *testing.B) {
			b.SetBytes(int64(size + 1))
			b.ReportAllocs()
			for b.Loop() {
				line, discarded, err := newOutputLineReader(
					&sizedLineReader{remaining: size},
					nil,
				).nextLine(
					64<<10,
					DiscardOversizeOutput,
				)
				if err != nil || !discarded || line != nil {
					b.Fatalf("discard = (%d bytes, %v, %v), want nil/discarded/nil", len(line), discarded, err)
				}
			}
		})
	}
}

func TestAgentRejectsInvalidOutputRetention(t *testing.T) {
	for _, lineBytes := range []int{-1, 0, maximumRetainedLineBytes + 1} {
		if got := RetentionBudgetForLineBytes(lineBytes); got != 0 {
			t.Errorf("RetentionBudgetForLineBytes(%d) = %d, want 0", lineBytes, got)
		}
	}
	for _, budget := range []int{-1, 1, MaximumMaxOutputRetentionBytes + 1} {
		_, err := Start(context.Background(), Options{
			Command:                 "sh",
			MaxOutputRetentionBytes: budget,
		})
		if !errors.Is(err, ErrInvalidOutputRetention) {
			t.Errorf("Start(MaxOutputRetentionBytes=%d) error = %v, want ErrInvalidOutputRetention", budget, err)
		}
	}
	_, err := Start(context.Background(), Options{
		Command:              "sh",
		OversizeOutputPolicy: OversizeOutputPolicy(255),
	})
	if !errors.Is(err, ErrInvalidOversizePolicy) {
		t.Errorf("Start(invalid OversizeOutputPolicy) error = %v, want ErrInvalidOversizePolicy", err)
	}
	_, err = Start(context.Background(), Options{
		Command:                "sh",
		MaxObservedOutputBytes: -1,
	})
	if !errors.Is(err, ErrInvalidObservedOutputLimit) {
		t.Errorf(
			"Start(MaxObservedOutputBytes=-1) error = %v, want ErrInvalidObservedOutputLimit",
			err,
		)
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
// back-pressure finding: readLoop now blocks on the unbuffered output send
// rather than silently dropping lines, so a consumer that never reads must not
// deadlock the reader — Kill must unblock it. We start an agent that emits many
// lines and NEVER drain a.Output(); readLoop parks on admission. Kill must
// return promptly and Done must close, proving no goroutine leak.
func TestKillUnblocksParkedReadLoop(t *testing.T) {
	// Emit ~1000 lines with no consumer so a.out fills and the readLoop parks on
	// its blocking send.
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
// lifecycle transition, Kill could call killProcessTree on a PID already
// reaped by readLoop — and possibly recycled by the kernel. Each iteration a
// short-lived agent exits while concurrent goroutines call Kill; deterministic
// lifecycle tests inject the winner ordering, while this real-process stress
// proves both paths remain race-clean and idempotent.
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
