package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

// noopHandler answers every ipc.Handler method with zero values. Enough to
// stand a bare ipc.Server up for discovery tests that only care about
// connect/bind behavior, not command semantics.
type noopHandler struct{}

func (noopHandler) HandleStatus(context.Context) (ipc.StatusReply, error) {
	return ipc.StatusReply{}, nil
}
func (noopHandler) HandleEnqueue(context.Context, ipc.EnqueueArgs) (ipc.EnqueueReply, error) {
	return ipc.EnqueueReply{}, nil
}
func (noopHandler) HandleStop(context.Context, ipc.StopArgs) error { return nil }
func (noopHandler) HandleReloadConfig(context.Context) error       { return nil }
func (noopHandler) HandleAttach(ctx context.Context, _ ipc.AttachArgs, _ func(json.RawMessage) error) error {
	<-ctx.Done()
	return nil
}

// startBareServer binds an ipc.Server directly at runtimeDir's endpoint,
// bypassing Acquire/Listener. Used to simulate "a live supervisor is
// listening" without pulling in the rest of Supervisor's store/session
// wiring.
func startBareServer(t *testing.T, runtimeDir string) *ipc.Server {
	t.Helper()
	socketPath, heartbeatPath, _ := endpointPaths(runtimeDir)
	srv, err := ipc.NewServer(ipc.ServerOptions{
		SocketPath:    socketPath,
		HeartbeatPath: heartbeatPath,
		Handler:       noopHandler{},
	})
	if err != nil {
		t.Fatalf("ipc.NewServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("ipc.Server.Start: %v", err)
	}
	return srv
}

func TestFind_NoSupervisor(t *testing.T) {
	runtimeDir := t.TempDir()
	_, err := Find(runtimeDir)
	if !errors.Is(err, ErrNoSupervisor) {
		t.Fatalf("Find() err = %v, want ErrNoSupervisor", err)
	}
}

func TestFind_LiveSupervisor(t *testing.T) {
	runtimeDir := t.TempDir()
	srv := startBareServer(t, runtimeDir)
	defer func() { _ = srv.Stop() }()

	client, err := Find(runtimeDir)
	if err != nil {
		t.Fatalf("Find() err = %v, want nil", err)
	}
	defer func() { _ = client.Close() }()

	if _, err := client.Status(context.Background()); err != nil {
		t.Fatalf("client.Status: %v", err)
	}
}

func TestAcquire_FreshSucceeds(t *testing.T) {
	runtimeDir := t.TempDir()
	l, err := Acquire(runtimeDir)
	if err != nil {
		t.Fatalf("Acquire() err = %v, want nil", err)
	}
	defer func() { _ = l.Release() }()

	if _, err := os.Stat(filepath.Join(runtimeDir, pidFileName)); err != nil {
		t.Errorf("pid lock file not created: %v", err)
	}
}

func TestAcquire_SecondFailsWhileFirstHolds(t *testing.T) {
	runtimeDir := t.TempDir()

	// A real supervisor binds the ipc socket AND holds the PID lock
	// together (Supervisor.Run does both via Acquire + ipc.NewServer). To
	// exercise the "second Acquire refuses" contract in isolation from
	// Supervisor, stand up the same pairing directly: Acquire the PID
	// lock/socket path, then start a live listener on that socket so a
	// second Acquire's connect-probe finds it genuinely alive.
	l1, err := Acquire(runtimeDir)
	if err != nil {
		t.Fatalf("first Acquire() err = %v, want nil", err)
	}
	defer func() { _ = l1.Release() }()

	srv, err := ipc.NewServer(ipc.ServerOptions{
		SocketPath:    l1.SocketPath,
		HeartbeatPath: l1.HeartbeatPath,
		Handler:       noopHandler{},
	})
	if err != nil {
		t.Fatalf("ipc.NewServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("ipc.Server.Start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	_, err = Acquire(runtimeDir)
	if !errors.Is(err, ErrSupervisorRunning) {
		t.Fatalf("second Acquire() err = %v, want ErrSupervisorRunning", err)
	}
}

func TestAcquireRequiresRuntimeDir(t *testing.T) {
	if _, err := Acquire(""); err == nil {
		t.Error("Acquire with empty runtimeDir: want error, got nil")
	}
}

func TestListenerReleaseIsIdempotent(t *testing.T) {
	runtimeDir := t.TempDir()
	l, err := Acquire(runtimeDir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	// A second Release on an already-released Listener must be a clean
	// no-op (pidLock is nil), not an error or a panic on a closed file.
	if err := l.Release(); err != nil {
		t.Errorf("second Release: want nil, got %v", err)
	}
}
