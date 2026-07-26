package agent

import (
	"context"
	"errors"
	"testing"
)

func TestStartRejectsAlreadyCanceledContextBeforeSpawn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, err := Start(ctx, Options{Command: "command-must-not-run"})
	if a != nil {
		t.Fatal("Start returned Agent for already-canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start error = %v, want context.Canceled", err)
	}
}
