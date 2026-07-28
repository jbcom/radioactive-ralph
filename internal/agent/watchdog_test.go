//go:build !windows

// Watchdog tests drive a real pty-backed agent (see agent_test.go's build
// note); native Windows has no pty, so they build only on Unix/WSL.
package agent

import (
	"bytes"
	"context"
	"regexp"
	"runtime"
	"testing"
	"time"
)

func TestDiscardedPrefixPipelineHasThreeConcurrentOwners(t *testing.T) {
	prefixAllocated := make(chan struct{}, discardedPrefixRetentionSlots)
	a := &Agent{
		out:       make(chan []byte),
		discarded: make(chan []byte),
		activity:  make(chan time.Time, 1),
	}

	watchCtx, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	signals := Watch(watchCtx, a, WatchdogConfig{StallTimeout: time.Minute})
	callbackStarted := make(chan byte, 1)
	releaseCallback := make(chan struct{})
	callbackDone := make(chan struct{})
	go func() {
		defer close(callbackDone)
		first, ok := <-signals
		if !ok || first.Kind != Progress || !first.Discarded ||
			len(first.Line) != maxDiscardedOutputPrefixBytes {
			callbackStarted <- 0
			return
		}
		callbackStarted <- first.Line[0]
		<-releaseCallback
		runtime.KeepAlive(first.Line)
		for signal := range signals {
			runtime.KeepAlive(signal.Line)
		}
	}()

	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		for markerIndex := range discardedPrefixRetentionSlots {
			marker := byte('A' + markerIndex)
			// This is readLoop's ownership boundary: allocate one independent
			// prefix, then offer it through Agent's unbuffered channel.
			prefix := bytes.Repeat([]byte{marker}, maxDiscardedOutputPrefixBytes)
			prefixAllocated <- struct{}{}
			select {
			case a.discarded <- prefix:
			case <-watchCtx.Done():
				return
			}
		}
	}()

	select {
	case marker := <-callbackStarted:
		if marker != 'A' {
			t.Fatalf("blocked callback owns marker %q, want A", marker)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("provider callback did not receive the first discarded prefix")
	}

	// A is still owned by the blocked callback. Reaching the final configured
	// marker proves every earlier unbuffered handoff completed, so all owner
	// slots reserved by the aggregate budget are simultaneously represented.
	for allocation := 1; allocation <= discardedPrefixRetentionSlots; allocation++ {
		select {
		case <-prefixAllocated:
		case <-time.After(5 * time.Second):
			t.Fatalf(
				"discarded prefix allocation %d was not reached in the proven pipeline schedule",
				allocation,
			)
		}
	}

	cancelWatch()
	close(releaseCallback)
	select {
	case <-callbackDone:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked provider callback did not drain after release")
	}
	select {
	case <-senderDone:
	case <-time.After(5 * time.Second):
		t.Fatal("readLoop-model sender did not release after Watch cancellation")
	}
}

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
				if len(s.Line) != 0 {
					t.Fatalf("Prompt retained %d bytes of terminal content", len(s.Line))
				}
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

func TestWatchdogUsesReadActivityTimestampForTrickledPartialLine(t *testing.T) {
	const (
		stallTimeout = 250 * time.Millisecond
		readInterval = 40 * time.Millisecond
		readCount    = 4
		earlyMargin  = 25 * time.Millisecond
	)
	a := &Agent{
		out:      make(chan []byte),
		activity: make(chan time.Time, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := Watch(ctx, a, WatchdogConfig{StallTimeout: stallTimeout})

	// Synchronize Watch startup before measuring the read-activity extension.
	// The initial line is only a handshake; the partial reads below never reach
	// Output and must carry liveness through Activity alone.
	go func() {
		a.out <- []byte("watch-ready\n")
	}()
	select {
	case sig := <-sigs:
		if sig.Kind != Progress {
			t.Fatalf("startup signal = %v, want Progress", sig.Kind)
		}
	case <-time.After(stallTimeout):
		t.Fatal("watchdog did not admit the startup handshake")
	}

	// Model readLoop observing partial-line bytes. No line reaches Output, but
	// each underlying read advances the watchdog from its actual timestamp.
	for range readCount {
		time.Sleep(readInterval)
		a.noteOutputActivity()
	}
	quietSince := time.Now()

	select {
	case sig := <-sigs:
		t.Fatalf(
			"watchdog emitted %v after %s of quiet; read activity should reset the %s timer",
			sig.Kind,
			time.Since(quietSince),
			stallTimeout,
		)
	case <-time.After(stallTimeout - earlyMargin):
	}
	select {
	case sig := <-sigs:
		if sig.Kind != Stall {
			t.Fatalf("first signal = %v, want Stall only after trickle stops", sig.Kind)
		}
	case <-time.After(earlyMargin + stallTimeout):
		t.Fatal("watchdog did not stall after trickled read activity stopped")
	}
}

// waitForBlockedSender blocks until the goroutine identified by gid is parked
// in a channel send, i.e. until a receive on that channel is guaranteed to
// succeed immediately. It reads the runtime's own goroutine dump and looks for
// the "chan send" wait state, which the scheduler sets exactly when the sender
// has enqueued itself in the channel's sendq.
//
// This is a real barrier. A fixed sleep is not: on a shared/oversubscribed CI
// vCPU the sender can be scheduled later than any chosen duration, leaving only
// the expired timer ready so the watchdog legitimately reports Stall and the
// test fails for a reason that has nothing to do with the behavior under test.
func waitForBlockedSender(t *testing.T, gid string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	buf := make([]byte, 1<<20)
	want := []byte("goroutine " + gid + " [chan send")
	for {
		n := runtime.Stack(buf, true)
		if bytes.Contains(buf[:n], want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("sender goroutine %s never parked in a channel send", gid)
		}
		runtime.Gosched()
	}
}

// goroutineID returns the calling goroutine's ID, parsed from its stack header.
func goroutineID() string {
	buf := make([]byte, 64)
	n := runtime.Stack(buf, false)
	// Header is "goroutine NNN [running]:"
	fields := bytes.Fields(buf[:n])
	return string(fields[1])
}

func TestWatchdogPrefersReadyOutputOverExpiredStall(t *testing.T) {
	for iteration := range 100 {
		a := &Agent{
			out:      make(chan []byte),
			activity: make(chan time.Time, 1),
		}
		line := []byte(`{"type":"item.completed"}` + "\n")
		senderID := make(chan string, 1)
		go func() {
			senderID <- goroutineID()
			a.out <- line
		}()
		// Wait for the unbuffered sender to actually PARK in a.out's sendq
		// before starting the already-expired timer, so both select cases are
		// genuinely ready. See waitForBlockedSender: a fixed sleep is not a
		// barrier and is what made this test flake on CI.
		waitForBlockedSender(t, <-senderID)

		ctx, cancel := context.WithCancel(context.Background())
		sigs := Watch(ctx, a, WatchdogConfig{StallTimeout: time.Nanosecond})
		if cap(a.Output()) != 0 || cap(sigs) != 0 {
			cancel()
			t.Fatalf("transport channels must be unbuffered: output=%d signals=%d", cap(a.Output()), cap(sigs))
		}
		select {
		case sig := <-sigs:
			if sig.Kind != Progress {
				cancel()
				t.Fatalf("iteration %d first signal = %v, want ready Progress before expired Stall", iteration, sig.Kind)
			}
			if len(sig.Line) == 0 || &sig.Line[0] != &line[0] {
				cancel()
				t.Fatalf("iteration %d Progress copied retained output instead of transferring its byte slice", iteration)
			}
		case <-time.After(time.Second):
			cancel()
			t.Fatalf("iteration %d watchdog did not surface ready output", iteration)
		}
		cancel()
		for range sigs {
			cancel()
		}
	}
}

func TestWatchdogDoesNotRefreshExpiredStallFromBackpressuredActivity(t *testing.T) {
	const stallTimeout = 30 * time.Millisecond
	a := &Agent{
		out:      make(chan []byte),
		activity: make(chan time.Time, 1),
	}
	sigs := Watch(context.Background(), a, WatchdogConfig{
		StallTimeout: stallTimeout,
	})

	// Once this send completes, Watch has received the line and is blocked
	// admitting Progress to the intentionally unread sigs channel.
	lineAccepted := make(chan struct{})
	go func() {
		a.out <- []byte("first\n")
		close(lineAccepted)
	}()
	select {
	case <-lineAccepted:
	case <-time.After(time.Second):
		t.Fatal("Watch did not accept the first line")
	}

	// Model readLoop observing the next partial record while Watch remains
	// downstream-backpressured. Its actual read time is deliberately allowed to
	// expire before Progress admission resumes.
	observedAt := time.Now()
	a.activity <- observedAt
	time.Sleep(3 * stallTimeout)

	select {
	case sig := <-sigs:
		if sig.Kind != Progress {
			t.Fatalf("first signal = %v, want Progress", sig.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("Watch did not release the blocked Progress")
	}

	start := time.Now()
	select {
	case sig := <-sigs:
		if sig.Kind != Stall {
			t.Fatalf("second signal = %v, want Stall", sig.Kind)
		}
		if elapsed := time.Since(start); elapsed >= stallTimeout {
			t.Fatalf("stale activity granted a fresh timeout: Stall took %s after read timestamp %s", elapsed, observedAt)
		}
	case <-time.After(stallTimeout + 100*time.Millisecond):
		t.Fatal("expired read-time deadline did not surface promptly")
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
