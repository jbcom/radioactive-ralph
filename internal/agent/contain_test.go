//go:build !windows

package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/contain"
)

// TestContainmentRootStopsAProviderWritingOutsideTheCheckout is the end-to-end
// proof that the guarantee reaches a real provider process.
//
// internal/contain proves the primitive works; this proves Ralph actually uses
// it. A containment package nothing calls is the same false assurance as the
// validation layer it was written to supplement.
func TestContainmentRootStopsAProviderWritingOutsideTheCheckout(t *testing.T) {
	if !contain.Available() {
		t.Skip("no write containment primitive on this platform")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escaped.txt")

	a, err := Start(context.Background(), Options{
		Command:         "/bin/sh",
		Args:            []string{"-c", "echo escaped > " + outside},
		Dir:             root,
		ContainmentRoot: root,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForExit(t, a)

	if _, statErr := os.Stat(outside); statErr == nil {
		t.Fatal("the provider wrote outside its containment root; declaring a " +
			"root that does not constrain the process is a false guarantee")
	}
}

// TestContainmentRootStillAllowsWritesInsideTheCheckout is the control: a
// boundary that blocked the task's own working tree would fail every turn.
func TestContainmentRootStillAllowsWritesInsideTheCheckout(t *testing.T) {
	if !contain.Available() {
		t.Skip("no write containment primitive on this platform")
	}
	root := t.TempDir()
	inside := filepath.Join(root, "result.txt")

	a, err := Start(context.Background(), Options{
		Command:         "/bin/sh",
		Args:            []string{"-c", "echo ok > " + inside},
		Dir:             root,
		ContainmentRoot: root,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForExit(t, a)

	if _, statErr := os.Stat(inside); statErr != nil {
		t.Fatalf("a write inside the root did not land: %v", statErr)
	}
}

// TestNoContainmentRootLeavesTheProcessUnwrapped keeps every existing caller
// working unchanged. Containment is opt-in per Start; a zero ContainmentRoot
// must behave exactly as before this existed.
func TestNoContainmentRootLeavesTheProcessUnwrapped(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "unconstrained.txt")

	a, err := Start(context.Background(), Options{
		Command: "/bin/sh",
		Args:    []string{"-c", "echo x > " + outside},
		Dir:     root,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForExit(t, a)

	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("an UNCONTAINED agent was constrained anyway: %v — containment "+
			"must stay opt-in so existing callers are unaffected", statErr)
	}
}

// TestContainmentRootMustBeAbsolute fails closed rather than guarding a
// directory resolved against whatever cwd the process inherits.
func TestContainmentRootMustBeAbsolute(t *testing.T) {
	if !contain.Available() {
		t.Skip("no write containment primitive on this platform")
	}
	_, err := Start(context.Background(), Options{
		Command:         "/bin/echo",
		Args:            []string{"hi"},
		Dir:             t.TempDir(),
		ContainmentRoot: "relative/root",
	})
	if !errors.Is(err, contain.ErrRootNotAbsolute) {
		t.Fatalf("err = %v, want ErrRootNotAbsolute", err)
	}
}

// waitForExit drains the agent and waits for the process, so the filesystem
// assertions below run after the write has either happened or been refused.
func waitForExit(t *testing.T, a *Agent) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = a.Wait()
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("agent did not exit")
	}
}
