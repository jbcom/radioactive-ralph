package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
)

func TestServiceLifecycleOverIPC(t *testing.T) {
	t.Setenv("RALPH_STATE_DIR", shortStateRoot(t))
	repo := t.TempDir()

	svc, err := NewService(Options{
		RepoPath:          repo,
		TickInterval:      25 * time.Millisecond,
		HeartbeatInterval: 25 * time.Millisecond,
		ShutdownTimeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Run(runCtx)
	}()

	paths, err := xdg.Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	endpoint, heartbeat := ipc.ServiceEndpoint(paths.Sessions)

	status, err := waitForStatus(endpoint, heartbeat, repo, errCh, 5*time.Second)
	if err != nil {
		cancel()
		t.Fatalf("waitForStatus: %v", err)
	}
	if status.RepoPath != repo {
		t.Fatalf("Status.RepoPath = %q, want %q", status.RepoPath, repo)
	}
	if status.PID <= 0 {
		t.Fatalf("Status.PID = %d, want > 0", status.PID)
	}

	if err := assertAttachStreamsServiceStart(endpoint); err != nil {
		cancel()
		t.Fatalf("assertAttachStreamsServiceStart: %v", err)
	}

	client, err := ipc.Dial(endpoint, time.Second)
	if err != nil {
		cancel()
		t.Fatalf("Dial stop client: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Stop(context.Background(), ipc.StopArgs{Graceful: true}); err != nil {
		cancel()
		t.Fatalf("Stop: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("service did not stop after Stop request")
	}
}

func assertAttachStreamsServiceStart(endpoint string) error {
	client, err := ipc.Dial(endpoint, time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	stopErr := errors.New("saw service.start")
	attachCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = client.Attach(attachCtx, func(raw json.RawMessage) error {
		var event struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			return err
		}
		if event.Kind == "service.start" {
			return stopErr
		}
		return nil
	})
	if errors.Is(err, stopErr) {
		return nil
	}
	return err
}

func shortStateRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ralph-state-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func waitForStatus(endpoint, heartbeat, repo string, errCh <-chan error, timeout time.Duration) (ipc.StatusReply, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			if err == nil {
				return ipc.StatusReply{}, context.Canceled
			}
			return ipc.StatusReply{}, err
		default:
		}
		if !ipc.SocketAlive(heartbeat, time.Minute) {
			lastErr = context.DeadlineExceeded
			time.Sleep(50 * time.Millisecond)
			continue
		}
		client, err := ipc.Dial(endpoint, 200*time.Millisecond)
		if err != nil {
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		status, err := client.Status(context.Background())
		_ = client.Close()
		if err == nil && status.RepoPath == repo {
			return status, nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	return ipc.StatusReply{}, lastErr
}
