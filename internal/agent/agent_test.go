package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

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
	for range a.Output() {
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
