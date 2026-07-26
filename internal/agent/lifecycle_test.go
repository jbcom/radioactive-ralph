package agent

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type reapResult struct {
	claimed bool
	err     error
}

func TestLifecycleTerminationOwnsReapingBeforeNaturalObserver(t *testing.T) {
	var terminateCalls atomic.Int32
	var cleanupCalls atomic.Int32
	terminateStarted := make(chan struct{})
	releaseTerminate := make(chan struct{})
	terminationDone := make(chan terminationClaim)
	naturalDone := make(chan reapResult)
	lifecycle := &processLifecycle{}

	go func() {
		terminationDone <- lifecycle.claimTermination(
			func() (bool, error) { return false, nil },
			func() terminationOutcome {
				terminateCalls.Add(1)
				close(terminateStarted)
				<-releaseTerminate
				return terminationOutcome{}
			},
		)
	}()
	<-terminateStarted

	go func() {
		claimed, err := lifecycle.claimNatural(func() error {
			cleanupCalls.Add(1)
			return nil
		})
		naturalDone <- reapResult{claimed: claimed, err: err}
	}()
	select {
	case <-naturalDone:
		t.Fatal("natural reaping passed an in-progress termination claim")
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseTerminate)
	claim := <-terminationDone
	if !claim.claimed || claim.natural || claim.cleanupErr != nil ||
		claim.terminationErr != nil {
		t.Fatalf("termination claim = %+v, want forced reaping ownership", claim)
	}
	result := <-naturalDone
	if result.claimed || result.err != nil {
		t.Fatalf("natural claim = %+v, want no-op after forced ownership", result)
	}
	if terminateCalls.Load() != 1 || cleanupCalls.Load() != 0 {
		t.Fatalf("raw calls = terminate %d cleanup %d, want 1/0",
			terminateCalls.Load(), cleanupCalls.Load())
	}

	lifecycle.finish(errors.New("signal: killed"))
	if err := lifecycle.exitError(); err == nil {
		t.Fatal("ExitErr = nil, want unclassified synthetic wait error preserved")
	}
}

func TestLifecycleNaturalObserverOwnsReapingBeforeTermination(t *testing.T) {
	naturalErr := errors.New("natural exit 23")
	var terminateCalls atomic.Int32
	cleanupStarted := make(chan struct{})
	releaseCleanup := make(chan struct{})
	naturalDone := make(chan reapResult)
	terminationDone := make(chan terminationClaim)
	lifecycle := &processLifecycle{}

	go func() {
		claimed, err := lifecycle.claimNatural(func() error {
			close(cleanupStarted)
			<-releaseCleanup
			return nil
		})
		naturalDone <- reapResult{claimed: claimed, err: err}
	}()
	<-cleanupStarted

	go func() {
		terminationDone <- lifecycle.claimTermination(
			func() (bool, error) { return false, nil },
			func() terminationOutcome {
				terminateCalls.Add(1)
				return terminationOutcome{}
			},
		)
	}()
	select {
	case <-terminationDone:
		t.Fatal("termination passed an in-progress natural claim")
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseCleanup)
	if result := <-naturalDone; !result.claimed || result.err != nil {
		t.Fatalf("natural claim = %+v, want ownership", result)
	}
	if claim := <-terminationDone; claim != (terminationClaim{}) {
		t.Fatalf("late termination claim = %+v, want no-op", claim)
	}
	lifecycle.finish(naturalErr)

	if !errors.Is(lifecycle.exitError(), naturalErr) {
		t.Fatalf("ExitErr = %v, want natural status", lifecycle.exitError())
	}
	if terminateCalls.Load() != 0 {
		t.Fatalf("late termination made %d raw calls", terminateCalls.Load())
	}
}

func TestLifecycleObservedExitRoutesToNaturalOwnership(t *testing.T) {
	lifecycle := &processLifecycle{}
	var terminateCalls atomic.Int32
	claim := lifecycle.claimTermination(
		func() (bool, error) { return true, nil },
		func() terminationOutcome {
			terminateCalls.Add(1)
			return terminationOutcome{}
		},
	)
	if !claim.natural || claim.claimed || terminateCalls.Load() != 0 {
		t.Fatalf("claim = %+v, terminate calls=%d; want natural handoff",
			claim, terminateCalls.Load())
	}
	claimed, err := lifecycle.claimNatural(func() error { return nil })
	if !claimed || err != nil {
		t.Fatalf("claimNatural = (%v, %v), want ownership", claimed, err)
	}
	naturalErr := errors.New("exit status 23")
	lifecycle.finish(naturalErr)
	if !errors.Is(lifecycle.exitError(), naturalErr) {
		t.Fatalf("ExitErr = %v, want status 23", lifecycle.exitError())
	}
}

func TestLifecycleProcessDoneDuringTerminationPreservesNaturalWaitStatus(t *testing.T) {
	lifecycle := &processLifecycle{}
	naturalErr := errors.New("exit status 23")
	var observeCalls atomic.Int32
	var terminateCalls atomic.Int32

	claim := lifecycle.claimTermination(
		func() (bool, error) {
			observeCalls.Add(1)
			return false, nil
		},
		func() terminationOutcome {
			terminateCalls.Add(1)
			return terminationOutcome{alreadyExited: true}
		},
	)
	if !claim.natural || claim.claimed || claim.observationErr != nil ||
		claim.cleanupErr != nil || claim.terminationErr != nil {
		t.Fatalf("claim = %+v, want natural handoff after process-done kill", claim)
	}
	if observeCalls.Load() != 1 || terminateCalls.Load() != 1 {
		t.Fatalf("calls = observe %d terminate %d, want 1/1",
			observeCalls.Load(), terminateCalls.Load())
	}

	claimed, err := lifecycle.claimNatural(func() error { return nil })
	if !claimed || err != nil {
		t.Fatalf("claimNatural = (%v, %v), want claimed", claimed, err)
	}
	lifecycle.finish(naturalErr)
	if !errors.Is(lifecycle.exitError(), naturalErr) {
		t.Fatalf("ExitErr = %v, want natural status", lifecycle.exitError())
	}
	if phase, forced, waitErr := lifecycle.snapshot(); phase != lifecycleFinished ||
		forced || !errors.Is(waitErr, naturalErr) {
		t.Fatalf("snapshot = (%v, forced=%v, err=%v), want finished/natural/status 23",
			phase, forced, waitErr)
	}
}

func TestLifecycleObserverFailureStillTransfersSuccessfulTermination(t *testing.T) {
	lifecycle := &processLifecycle{}
	observerErr := errors.New("injected observer failure")
	claim := lifecycle.claimTermination(
		func() (bool, error) { return false, observerErr },
		func() terminationOutcome { return terminationOutcome{} },
	)
	if !claim.claimed || !errors.Is(claim.observationErr, observerErr) ||
		claim.terminationErr != nil {
		t.Fatalf("claim = %+v, want forced ownership plus observer error", claim)
	}
	phase, forced, _ := lifecycle.snapshot()
	if phase != lifecycleReaping || !forced {
		t.Fatalf("snapshot = (%v, forced=%v), want forced/reaping", phase, forced)
	}
}

func TestLifecycleTerminationFailureHasExplicitTerminalPhase(t *testing.T) {
	lifecycle := &processLifecycle{}
	terminationErr := errors.New("injected stable-handle kill failure")
	claim := lifecycle.claimTermination(
		func() (bool, error) { return false, nil },
		func() terminationOutcome {
			return terminationOutcome{terminationErr: terminationErr}
		},
	)
	if claim.claimed || !errors.Is(claim.terminationErr, terminationErr) {
		t.Fatalf("claim = %+v, want explicit failure", claim)
	}
	phase, forced, _ := lifecycle.snapshot()
	if phase != lifecycleFailed || forced {
		t.Fatalf("snapshot = (%v, forced=%v), want failed/unforced", phase, forced)
	}

	var lateCalls atomic.Int32
	late := lifecycle.claimTermination(
		func() (bool, error) {
			lateCalls.Add(1)
			return false, nil
		},
		func() terminationOutcome {
			lateCalls.Add(1)
			return terminationOutcome{}
		},
	)
	if late != (terminationClaim{}) || lateCalls.Load() != 0 {
		t.Fatalf("late claim = %+v, raw calls=%d; want terminal no-op", late, lateCalls.Load())
	}
}

func TestLifecycleNaturalCleanupFailureStillTransfersWaitOwnership(t *testing.T) {
	lifecycle := &processLifecycle{}
	cleanupErr := errors.New("injected descendant cleanup failure")
	claimed, err := lifecycle.claimNatural(func() error { return cleanupErr })
	if !claimed || !errors.Is(err, cleanupErr) {
		t.Fatalf("claimNatural = (%v, %v), want owned reaping plus error", claimed, err)
	}
	if phase, forced, _ := lifecycle.snapshot(); phase != lifecycleReaping || forced {
		t.Fatalf("snapshot = (%v, forced=%v), want natural/reaping", phase, forced)
	}
	lifecycle.finish(nil)
}
