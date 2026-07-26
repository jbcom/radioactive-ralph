//go:build aix || dragonfly || freebsd || netbsd || openbsd || solaris

package agent

import (
	"context"
	"errors"
	"testing"
)

func TestUnsupportedExitObserverHostIsRejectedBeforeSpawn(t *testing.T) {
	a, err := Start(context.Background(), Options{Command: "must-not-run"})
	if a != nil || !errors.Is(err, ErrProcessExitObservationUnsupported) {
		t.Fatalf("Start = (%v, %v), want explicit observer-host rejection", a, err)
	}
}
