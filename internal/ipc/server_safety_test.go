//go:build !windows

package ipc

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

// newTestServer starts a server and returns it (so a test can time Stop
// directly) plus its socket path.
func newTestServer(t *testing.T, h Handler) (*Server, string) {
	t.Helper()
	dir := shortTempDir(t)
	socketPath, heartbeatPath := ServiceEndpoint(dir)
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
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return srv, socketPath
}

// TestServerStopNotBlockedByHalfOpenClient is the regression for the IPC
// audit's C1: a client that dials and sends NOTHING (no newline) must not park
// a handler goroutine forever and hang Stop()'s wg.Wait. The request read
// deadline turns the stuck read into a prompt error so the handler returns and
// Stop drains.
func TestServerStopNotBlockedByHalfOpenClient(t *testing.T) {
	srv, sock := newTestServer(t, &fakeHandler{})

	// Dial and send nothing — a half-open client.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Give the accept loop a moment to spawn the handler that's now parked in
	// its request read.
	time.Sleep(50 * time.Millisecond)

	// Stop must drain PROMPTLY: it closes the tracked connection, which unblocks
	// the parked read at once (rather than waiting out the 30s read deadline).
	done := make(chan error, 1)
	go func() { done <- srv.Stop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() hung on a half-open client — the handler was not released by closing its connection")
	}
}

// TestServerRejectsOversizedRequest is the regression for C2: a request line
// larger than maxRequestBytes must be BOUNDED (LimitReader), so the server
// stops reading at the cap and closes the connection — it must not accumulate
// an unbounded buffer nor hang. The client streams the cap's worth of bytes
// with no newline; the read side must then observe the connection CLOSED
// (io.EOF), not a hang (which would surface as a read timeout below and fail).
func TestServerRejectsOversizedRequest(t *testing.T) {
	srv, sock := newTestServer(t, &fakeHandler{})
	defer func() { _ = srv.Stop() }()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Stream exactly the cap's worth of non-newline bytes. The LimitReader
	// yields io.EOF to the server's ReadBytes at the limit (no newline seen), so
	// its handler returns and closes the conn. Writing more than the cap is
	// unnecessary (and slow); the limit itself is the trigger.
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	big := strings.Repeat("a", maxRequestBytes)
	_, _ = conn.Write([]byte(big)) // may partially write before the server closes

	// Drain until the connection is closed by the server. A read MUST eventually
	// return io.EOF (or another close error) — NOT a timeout, which would mean
	// the server hung reading unbounded input.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 4096)
	for {
		_, err := conn.Read(buf)
		if err == nil {
			continue // the server may write a rejection Response first; keep draining
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Fatal("read timed out — the server hung on an oversized request instead of bounding it and closing the connection")
		}
		// io.EOF or another close error: the server bounded the request and
		// closed the conn. Correct.
		return
	}
}

// TestServerRejectsNewerProtocolVersion is the regression for C5: an observe
// request from a client claiming a newer protocol version than the server
// speaks must be rejected with an unsupported_command Code, not decoded and
// served as the server's version.
func TestServerRejectsNewerProtocolVersion(t *testing.T) {
	srv, sock := newTestServer(t, &fakeHandler{})
	defer func() { _ = srv.Stop() }()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// A status request one version ahead of the server.
	req := Request{Cmd: CmdStatus, ProtoVersion: ProtoVersion + 1}
	frame, err := encodeJSONLine(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	respLine, err := readLine(conn)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(respLine, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Ok {
		t.Fatal("newer-version status request was served OK; want rejected")
	}
	if resp.Code != CodeUnsupportedCommand {
		t.Errorf("Code = %q, want %q for a too-new protocol version", resp.Code, CodeUnsupportedCommand)
	}
}

// stopTriggeringHandler embeds fakeHandler and makes HandleStop trigger the
// server's own Stop() (as the real supervisor's run loop does on stopCh),
// racing the stop response's write against closeConns.
type stopTriggeringHandler struct {
	fakeHandler
	srv *Server
}

func (h *stopTriggeringHandler) HandleStop(ctx context.Context, a StopArgs) error {
	// Kick off the server shutdown concurrently — this is what closes conns and
	// could race the stop response's write — then wait long enough that
	// closeConns has DEFINITELY run before we return and writeResponse fires.
	// This makes the race deterministic: without the stopConn skip, this conn is
	// already closed by the time the response is written, and client.Stop errors.
	go func() { _ = h.srv.Stop() }()
	time.Sleep(200 * time.Millisecond)
	return h.fakeHandler.HandleStop(ctx, a)
}

// TestServerStopRequestStillGetsResponse is the regression for the CodeRabbit
// P1: a CmdStop that triggers the server's own Stop() must still deliver its
// response to the requesting client, not close that client's connection out
// from under the unwritten reply (which would surface as ErrClosed even though
// the supervisor stopped cleanly). closeConns skips the stop-requesting conn.
func TestServerStopRequestStillGetsResponse(t *testing.T) {
	h := &stopTriggeringHandler{}
	srv, sock := newTestServer(t, h)
	h.srv = srv

	c := dialTest(t, sock)
	defer func() { _ = c.Close() }()

	done := make(chan error, 1)
	go func() { done <- c.Stop(context.Background(), StopArgs{}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("client.Stop = %v, want nil — the stop response was lost to a closed connection", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client.Stop did not return — the response was never delivered")
	}
}

// readLine reads a single newline-delimited frame from conn.
func readLine(conn net.Conn) ([]byte, error) {
	var out []byte
	buf := make([]byte, 1)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return out, nil
			}
			out = append(out, buf[0])
		}
		if err != nil {
			return out, err
		}
	}
}
