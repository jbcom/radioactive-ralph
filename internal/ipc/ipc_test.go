package ipc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// dialRaw opens a raw Unix socket connection (for tests that exercise
// the wire protocol directly rather than via Client).
func dialRaw(socketPath string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return dialEndpoint(ctx, socketPath, timeout)
}

// fakeHandler is a test double implementing Handler with recorded
// call counts and configurable replies.
type fakeHandler struct {
	mu           sync.Mutex
	statusCount  atomic.Int64
	enqueueCount atomic.Int64
	stopCount    atomic.Int64
	reloadCount  atomic.Int64
	attachCount  atomic.Int64
	statusReply  StatusReply
	enqueueReply EnqueueReply
	stopErr      error
	reloadErr    error
	attachEvents [][]byte
	attachDelay  time.Duration
	attachErr    error
}

func (f *fakeHandler) HandleStatus(_ context.Context) (StatusReply, error) {
	f.statusCount.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statusReply, nil
}

func (f *fakeHandler) HandleEnqueue(_ context.Context, _ EnqueueArgs) (EnqueueReply, error) {
	f.enqueueCount.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enqueueReply, nil
}

func (f *fakeHandler) HandleStop(_ context.Context, _ StopArgs) error {
	f.stopCount.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopErr
}

func (f *fakeHandler) HandleReloadConfig(_ context.Context) error {
	f.reloadCount.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reloadErr
}

func (f *fakeHandler) HandleAttach(ctx context.Context, emit func(json.RawMessage) error) error {
	f.attachCount.Add(1)
	f.mu.Lock()
	events := f.attachEvents
	delay := f.attachDelay
	retErr := f.attachErr
	f.mu.Unlock()

	for _, e := range events {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		if err := emit(e); err != nil {
			return err
		}
	}
	return retErr
}

// shortTempDir returns a short-path temp directory. macOS Unix sockets
// have a 104-byte path limit and the default t.TempDir() under
// /var/folders/... can exceed that, so we use /tmp/ipc-<suffix> instead.
func shortTempDir(t *testing.T) string {
	t.Helper()
	base := os.TempDir()
	if runtime.GOOS == "darwin" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "ipct-*")
	if err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startServer spins up a server bound to a tmp socket + heartbeat file.
func startServer(t *testing.T, h Handler) (socketPath, heartbeatPath string, cleanup func()) {
	t.Helper()
	dir := shortTempDir(t)
	socketPath, heartbeatPath = ServiceEndpoint(dir)

	srv, err := NewServer(ServerOptions{
		SocketPath:        socketPath,
		HeartbeatPath:     heartbeatPath,
		HeartbeatInterval: 20 * time.Millisecond,
		Handler:           h,
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(20 * time.Millisecond); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return socketPath, heartbeatPath, func() {
		if err := srv.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}
}

func TestNewServerValidation(t *testing.T) {
	cases := []struct {
		name string
		opts ServerOptions
	}{
		{"missing socket", ServerOptions{HeartbeatPath: "/tmp/a", Handler: &fakeHandler{}}},
		{"missing heartbeat", ServerOptions{SocketPath: "/tmp/s", Handler: &fakeHandler{}}},
		{"missing handler", ServerOptions{SocketPath: "/tmp/s", HeartbeatPath: "/tmp/a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewServer(c.opts); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestRoundTripStatus(t *testing.T) {
	h := &fakeHandler{
		statusReply: StatusReply{
			RepoPath:      "/tmp/repo",
			PID:           12345,
			ActiveWorkers: 3,
			ReadyTasks:    2,
			ApprovalTasks: 1,
			RunningTasks:  1,
			FailedTasks:   4,
			ActivePlans:   1,
		},
	}
	socketPath, _, cleanup := startServer(t, h)
	defer cleanup()

	c, err := Dial(socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	reply, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if reply.RepoPath != "/tmp/repo" || reply.PID != 12345 {
		t.Errorf("reply = %+v", reply)
	}
	if h.statusCount.Load() != 1 {
		t.Errorf("expected 1 status call, got %d", h.statusCount.Load())
	}
}

func TestRoundTripEnqueue(t *testing.T) {
	h := &fakeHandler{
		enqueueReply: EnqueueReply{TaskID: "t-42", Inserted: true},
	}
	socketPath, _, cleanup := startServer(t, h)
	defer cleanup()

	c, err := Dial(socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	reply, err := c.Enqueue(context.Background(), EnqueueArgs{
		Description: "fix the thing",
		Priority:    3,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if reply.TaskID != "t-42" || !reply.Inserted {
		t.Errorf("reply = %+v", reply)
	}
}

func TestRoundTripStopAndReload(t *testing.T) {
	h := &fakeHandler{}
	socketPath, _, cleanup := startServer(t, h)
	defer cleanup()

	c1, err := Dial(socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c1.Close() }()

	if err := c1.ReloadConfig(context.Background()); err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}

	c2, err := Dial(socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial 2: %v", err)
	}
	defer func() { _ = c2.Close() }()

	if err := c2.Stop(context.Background(), StopArgs{Graceful: true}); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if h.stopCount.Load() != 1 || h.reloadCount.Load() != 1 {
		t.Errorf("call counts wrong: stop=%d reload=%d",
			h.stopCount.Load(), h.reloadCount.Load())
	}
}

func TestRoundTripAttachStreamsEvents(t *testing.T) {
	h := &fakeHandler{
		attachEvents: [][]byte{
			[]byte(`{"kind":"message.user","body":"hello"}`),
			[]byte(`{"kind":"session.spawned","uuid":"abc"}`),
			[]byte(`{"kind":"task.done","id":"t1"}`),
		},
	}
	socketPath, _, cleanup := startServer(t, h)
	defer cleanup()

	c, err := Dial(socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	var received []json.RawMessage
	err = c.Attach(context.Background(), func(e json.RawMessage) error {
		// Copy because the reader's buffer may be reused.
		buf := append(json.RawMessage(nil), e...)
		received = append(received, buf)
		return nil
	})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(received) != 3 {
		t.Fatalf("expected 3 events, got %d", len(received))
	}
	// Basic sanity check on one of them.
	var parsed map[string]any
	if err := json.Unmarshal(received[0], &parsed); err != nil {
		t.Errorf("unmarshal event: %v", err)
	}
	if parsed["kind"] != "message.user" {
		t.Errorf("event[0].kind = %v", parsed["kind"])
	}
}

func TestRoundTripAttachCancellation(t *testing.T) {
	h := &fakeHandler{
		attachEvents: [][]byte{
			[]byte(`{"kind":"e1"}`),
			[]byte(`{"kind":"e2"}`),
		},
		attachDelay: 100 * time.Millisecond, // slow stream so cancel lands mid-flight
	}
	socketPath, _, cleanup := startServer(t, h)
	defer cleanup()

	c, err := Dial(socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Client cancels before server finishes streaming. Expect a
	// context-related error OR ErrClosed (server closed on its side).
	attachErr := c.Attach(ctx, func(_ json.RawMessage) error { return nil })
	if attachErr == nil {
		t.Error("expected cancellation or closed error")
	}
}

func TestMalformedRequest(t *testing.T) {
	h := &fakeHandler{}
	socketPath, _, cleanup := startServer(t, h)
	defer cleanup()

	// Open a raw connection and write garbage instead of a proper Request.
	conn, err := dialRaw(socketPath, time.Second)
	if err != nil {
		t.Fatalf("dialRaw: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte("this is not json\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(buf[:n-1], &resp); err != nil { // strip trailing \n
		t.Fatalf("decode: %v", err)
	}
	if resp.Ok || resp.Error == "" {
		t.Errorf("expected error response, got %+v", resp)
	}
}

func TestUnknownCommand(t *testing.T) {
	h := &fakeHandler{}
	socketPath, _, cleanup := startServer(t, h)
	defer cleanup()

	conn, err := dialRaw(socketPath, time.Second)
	if err != nil {
		t.Fatalf("dialRaw: %v", err)
	}
	defer func() { _ = conn.Close() }()

	req := Request{Cmd: "nope"}
	body, _ := json.Marshal(req)
	body = append(body, '\n')
	if _, err := conn.Write(body); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(buf[:n-1], &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Ok || resp.Error == "" {
		t.Errorf("expected error response, got %+v", resp)
	}
}

func TestHeartbeatIsWritten(t *testing.T) {
	h := &fakeHandler{}
	_, heartbeatPath, cleanup := startServer(t, h)
	defer cleanup()

	// Wait up to 500ms for the heartbeat file to appear.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if SocketAlive(heartbeatPath, 100*time.Millisecond) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("heartbeat file never became alive")
}

func TestSocketAliveStale(t *testing.T) {
	// Nonexistent file is always "not alive".
	if SocketAlive("/does/not/exist", time.Second) {
		t.Error("nonexistent path should not be reported alive")
	}
}

func TestServiceEndpointUnix(t *testing.T) {
	endpoint, heartbeat := serviceEndpointForGOOS("linux", "/tmp/ralph/sessions")
	if endpoint != "/tmp/ralph/sessions/service.sock" {
		t.Fatalf("endpoint = %q", endpoint)
	}
	if heartbeat != "/tmp/ralph/sessions/service.sock.alive" {
		t.Fatalf("heartbeat = %q", heartbeat)
	}
}

func TestServiceEndpointWindows(t *testing.T) {
	sessionsDir := `C:\Users\me\AppData\Local\radioactive-ralph\sessions`
	endpoint, heartbeat := serviceEndpointForGOOS("windows", sessionsDir)
	sum := sha256.Sum256([]byte(sessionsDir))
	token := hex.EncodeToString(sum[:])[:12]
	wantEndpoint := `\\.\pipe\radioactive_ralph-` + token + `-service`
	if endpoint != wantEndpoint {
		t.Fatalf("endpoint = %q, want %q", endpoint, wantEndpoint)
	}
	if !strings.HasSuffix(strings.ReplaceAll(heartbeat, `\`, `/`), "radioactive-ralph/sessions/service.alive") {
		t.Fatalf("heartbeat = %q", heartbeat)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	h := &fakeHandler{}
	dir := shortTempDir(t)
	socketPath, heartbeat := ServiceEndpoint(dir)
	srv, err := NewServer(ServerOptions{
		SocketPath:    socketPath,
		HeartbeatPath: heartbeat,
		Handler:       h,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(100 * time.Millisecond); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := srv.Stop(); err != nil {
		t.Errorf("first Stop: %v", err)
	}
	if err := srv.Stop(); err != nil {
		t.Errorf("second Stop should be idempotent: %v", err)
	}
}

func TestDialNonexistentSocket(t *testing.T) {
	_, err := Dial("/nonexistent/socket", 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected dial error")
	}
}

func TestClientCloseStopsReading(t *testing.T) {
	h := &fakeHandler{}
	socketPath, _, cleanup := startServer(t, h)
	defer cleanup()

	c, err := Dial(socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = c.Close()

	// After close, Status should error cleanly (not hang).
	_, err = c.Status(context.Background())
	if err == nil {
		t.Error("expected error after close")
	}
}

func TestErrClosedIdentity(t *testing.T) {
	if !errors.Is(ErrClosed, ErrClosed) {
		t.Error("errors.Is should match ErrClosed")
	}
}
