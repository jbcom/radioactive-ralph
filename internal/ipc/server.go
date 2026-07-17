package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"
)

// acceptRetryDelay is the pause after a transient Accept() error before
// retrying, so a persistent condition (e.g. sustained fd exhaustion) backs
// off instead of busy-spinning the accept goroutine.
const acceptRetryDelay = 50 * time.Millisecond

// defaultHeartbeatInterval is the heartbeat-touch cadence when a caller
// leaves ServerOptions.HeartbeatInterval unset.
const defaultHeartbeatInterval = 10 * time.Second

// Per-connection safety bounds. Clients are dumb and short-lived (dial → one
// request line → response → close), so a request that hasn't fully arrived
// within requestReadTimeout, or a response that can't be flushed within
// responseWriteTimeout, means a slow/half-open/uncooperative client — NOT
// legitimate work. Without these a bad client leaks its conn goroutine + wg
// slot (hanging Stop()'s wg.Wait forever), and an unbounded request read lets
// one client OOM the supervisor. Attach re-arms its own per-frame write
// deadline; the request-read deadline is cleared once its request is parsed.
const (
	requestReadTimeout   = 30 * time.Second
	responseWriteTimeout = 30 * time.Second
	// maxRequestBytes caps a single request line so a client can't stream an
	// unbounded body with no newline and exhaust server memory. Most requests
	// are tiny JSON control messages, but CmdPlanImport embeds a whole
	// operator-supplied markdown plan file in its args — so the cap must be
	// large enough for a realistic plan while still bounding a runaway client.
	// 32MiB comfortably fits any hand-authored plan and its JSON escaping.
	maxRequestBytes = 32 << 20
)

// Handler handles a single client request. Attach streams events by
// calling emit repeatedly; other commands return (reply, nil) and the
// server transmits a single Response frame.
type Handler interface {
	// HandleStatus returns the current repo-service status.
	HandleStatus(ctx context.Context) (StatusReply, error)

	// HandleEnqueue queues a new task, returning the task ID and whether
	// it was a fresh insert or a dedup hit.
	HandleEnqueue(ctx context.Context, args EnqueueArgs) (EnqueueReply, error)

	// HandleStop signals the repo service to shut down. The server closes
	// the IPC socket after sending the response.
	HandleStop(ctx context.Context, args StopArgs) error

	// HandleReloadConfig asks the repo service to re-read config.toml.
	HandleReloadConfig(ctx context.Context) error

	// HandleAttach streams events to the client until either the
	// service exits or the client disconnects. The implementation
	// should return when ctx is cancelled.
	HandleAttach(ctx context.Context, emit func(json.RawMessage) error) error
}

// DriveHandler is the OPTIONAL v2 drive surface. A Handler that also
// implements DriveHandler gains the plan-import/plan-set-status/task-approve/
// worker-kill commands; one that does not still serves the v1 observe surface,
// and the server answers a drive command with an unsupported_command response.
// Keeping it a separate interface means existing v1 Handler implementations
// (and their test doubles) compile unchanged.
type DriveHandler interface {
	// HandlePlanImport creates + activates a plan from markdown.
	HandlePlanImport(ctx context.Context, args PlanImportArgs) (PlanImportReply, error)
	// HandlePlanSetStatus changes a plan's lifecycle status (validated).
	HandlePlanSetStatus(ctx context.Context, args PlanSetStatusArgs) (PlanSetStatusReply, error)
	// HandleTaskApprove clears the approval gate on a ready_pending_approval task.
	HandleTaskApprove(ctx context.Context, args TaskApproveArgs) error
	// HandleWorkerKill kills a running worker via kill-and-reclaim.
	HandleWorkerKill(ctx context.Context, args WorkerKillArgs) error
}

// Server is the repo-service IPC server. One instance per repo service.
type Server struct {
	socketPath        string
	heartbeatPath     string
	listener          net.Listener
	handler           Handler
	logger            *slog.Logger
	heartbeatInterval time.Duration
	wg                sync.WaitGroup
	stopCh            chan struct{}
	heartbeatCh       chan struct{}

	// conns tracks every accepted connection so Stop can close them all,
	// unblocking any handler parked in a read/write immediately — so shutdown
	// drains promptly even against a half-open or non-reading client, rather
	// than waiting out that connection's per-op deadline. connsMu guards it.
	//
	// stopConn, when non-nil, is the connection whose CmdStop request TRIGGERED
	// this shutdown. closeConns skips it so its handler can still write the stop
	// response before its own deferred Close runs — otherwise the requester
	// would see ErrClosed even though the supervisor stopped cleanly, breaking
	// the documented stop-command reply contract.
	connsMu  sync.Mutex
	conns    map[net.Conn]struct{}
	stopConn net.Conn
}

// ServerOptions configures a Server.
type ServerOptions struct {
	// SocketPath is the local endpoint path to bind. Typically
	// state/<repo>/sessions/service.sock on POSIX hosts.
	SocketPath string

	// HeartbeatPath is the file whose mtime is bumped every
	// HeartbeatInterval by the server. Typically the repo-service
	// heartbeat file next to the endpoint metadata.
	HeartbeatPath string

	// HeartbeatInterval controls how often we refresh the heartbeat.
	// Defaults to 10s when zero.
	HeartbeatInterval time.Duration

	// Handler satisfies the IPC Handler interface.
	Handler Handler

	// Logger receives info/warn/error messages. Defaults to a no-op
	// logger when nil.
	Logger *slog.Logger
}

// NewServer constructs a Server. It does NOT bind the socket — call
// Start to begin accepting connections.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.SocketPath == "" {
		return nil, errors.New("ipc: SocketPath required")
	}
	if opts.HeartbeatPath == "" {
		return nil, errors.New("ipc: HeartbeatPath required")
	}
	if opts.Handler == nil {
		return nil, errors.New("ipc: Handler required")
	}
	interval := opts.HeartbeatInterval
	if interval == 0 {
		interval = defaultHeartbeatInterval
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		socketPath:        opts.SocketPath,
		heartbeatPath:     opts.HeartbeatPath,
		handler:           opts.Handler,
		logger:            logger,
		heartbeatInterval: interval,
		stopCh:            make(chan struct{}),
		heartbeatCh:       make(chan struct{}),
		conns:             make(map[net.Conn]struct{}),
	}, nil
}

// trackConn registers a live connection (or, if the server is already
// stopping, closes it immediately and returns false so the caller doesn't
// serve a request during shutdown). deregister via untrackConn.
func (s *Server) trackConn(conn net.Conn) bool {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	select {
	case <-s.stopCh:
		return false
	default:
	}
	s.conns[conn] = struct{}{}
	return true
}

func (s *Server) untrackConn(conn net.Conn) {
	s.connsMu.Lock()
	delete(s.conns, conn)
	s.connsMu.Unlock()
}

// closeConns closes every tracked connection (except the one whose CmdStop
// triggered this shutdown, if any), unblocking any handler parked in a
// read/write so Stop's wg.Wait drains promptly. The skipped stop-requester's
// handler writes its response and then closes its own conn via its defer.
func (s *Server) closeConns() {
	s.connsMu.Lock()
	for c := range s.conns {
		if c == s.stopConn {
			continue
		}
		_ = c.Close()
	}
	s.connsMu.Unlock()
}

// markStopConn records the connection currently serving a CmdStop so closeConns
// won't close it out from under its unwritten response.
func (s *Server) markStopConn(conn net.Conn) {
	s.connsMu.Lock()
	s.stopConn = conn
	s.connsMu.Unlock()
}

// Start binds the socket and begins accepting connections in a background
// goroutine. Safe to call once. Returns the listener error if bind fails.
// The heartbeat interval comes from ServerOptions.HeartbeatInterval (set at
// NewServer), not a parameter here — a single source of truth.
func (s *Server) Start() error {
	l, err := listenEndpoint(s.socketPath)
	if err != nil {
		return fmt.Errorf("ipc: listen %s: %w", s.socketPath, err)
	}
	s.listener = l

	s.wg.Add(2)
	go s.heartbeatLoop(s.heartbeatInterval)
	go s.acceptLoop()

	return nil
}

// Stop shuts the server down and waits for goroutines to exit.
func (s *Server) Stop() error {
	select {
	case <-s.stopCh:
		// already stopped
	default:
		close(s.stopCh)
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	// Close every live connection so any handler parked in a read/write returns
	// at once — otherwise wg.Wait would block until each stuck conn hit its own
	// deadline (or forever, before deadlines existed). This is what makes Stop
	// drain promptly even against a half-open or non-reading client.
	s.closeConns()
	s.wg.Wait()
	_ = cleanupEndpoint(s.socketPath)
	return nil
}

func (s *Server) heartbeatLoop(interval time.Duration) {
	defer s.wg.Done()
	// interval is always non-zero: NewServer applies defaultHeartbeatInterval.
	// Touch once at start so radioactive_ralph status sees a fresh heartbeat
	// immediately after the service comes up.
	_ = touchFile(s.heartbeatPath)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			if err := touchFile(s.heartbeatPath); err != nil {
				s.logger.Warn("ipc heartbeat touch failed", "err", err)
			}
		}
	}
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
			}
			// A closed listener is the only FATAL case (shutdown, already
			// handled above via stopCh, or the listener closed out from under
			// us) — stop accepting. Every OTHER Accept error (EMFILE/ENFILE
			// "too many open files", ECONNABORTED, EINTR, ...) is transient:
			// log it and KEEP accepting, or a momentary fd-pressure blip would
			// permanently wedge the entire control plane. A short backoff
			// avoids busy-spinning if the condition persists. This mirrors the
			// stdlib http.Server.Serve retry loop.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			s.logger.Warn("ipc accept error (transient; continuing)", "err", err)
			select {
			case <-s.stopCh:
				return
			case <-time.After(acceptRetryDelay):
			}
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

// handleConn reads a single JSON-line Request and dispatches to the
// Handler. For CmdAttach, multiple frames are emitted on the same
// connection until the client disconnects or the service stops.
func (s *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	// Register so Stop can close this conn and unblock us instantly on shutdown.
	// If the server is already stopping, don't serve — just return (conn is
	// closed by the defer above).
	if !s.trackConn(conn) {
		return
	}
	defer s.untrackConn(conn)

	// Bound the request read: a half-open client that never sends a newline
	// must not park this goroutine (and its wg slot) forever — that would leak
	// it and hang Stop()'s wg.Wait. The LimitReader caps how much one client can
	// stream before the newline, so a client can't OOM the server either.
	_ = conn.SetReadDeadline(time.Now().Add(requestReadTimeout))
	reader := bufio.NewReader(io.LimitReader(conn, maxRequestBytes))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		if !errors.Is(err, io.EOF) {
			s.logger.Debug("ipc read request", "err", err)
		}
		return
	}
	// Request is in hand: clear the read deadline. A streaming Attach lives far
	// longer than requestReadTimeout, and a request/response conn does no
	// further reads, so leaving the deadline set would false-trip either.
	_ = conn.SetReadDeadline(time.Time{})
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		s.writeResponse(conn, Response{Error: fmt.Sprintf("malformed request: %v", err)})
		return
	}

	// Reject a request from a newer protocol than this server speaks BEFORE
	// dispatch — a future version may reuse a command name with changed payload
	// semantics, so decoding it as our version would mis-execute it. (dispatchDrive
	// double-checks for the drive commands; this covers the observe/enqueue/stop
	// surface too.)
	if req.ProtoVersion > ProtoVersion {
		s.writeResponse(conn, Response{
			Ok:    false,
			Code:  CodeUnsupportedCommand,
			Error: fmt.Sprintf("protocol version %d not supported (this supervisor speaks v%d)", req.ProtoVersion, ProtoVersion),
		})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Watch for server stop while this handler is running.
	go func() {
		select {
		case <-ctx.Done():
		case <-s.stopCh:
			cancel()
		}
	}()

	switch req.Cmd {
	case CmdStatus:
		reply, err := s.handler.HandleStatus(ctx)
		s.writeResult(conn, reply, err)
	case CmdEnqueue:
		var args EnqueueArgs
		if err := json.Unmarshal(req.Args, &args); err != nil && len(req.Args) > 0 {
			s.writeResponse(conn, Response{Error: fmt.Sprintf("decode enqueue args: %v", err)})
			return
		}
		reply, err := s.handler.HandleEnqueue(ctx, args)
		s.writeResult(conn, reply, err)
	case CmdStop:
		var args StopArgs
		if err := json.Unmarshal(req.Args, &args); err != nil && len(req.Args) > 0 {
			s.writeResponse(conn, Response{Error: fmt.Sprintf("decode stop args: %v", err)})
			return
		}
		// HandleStop triggers the supervisor's shutdown, which calls our own
		// Stop() → closeConns() — possibly before we write the reply below.
		// Mark this conn so closeConns skips it, preserving the stop response.
		s.markStopConn(conn)
		err := s.handler.HandleStop(ctx, args)
		s.writeResponse(conn, Response{Ok: err == nil, Error: errString(err)})
	case CmdReloadConfig:
		err := s.handler.HandleReloadConfig(ctx)
		s.writeResponse(conn, Response{Ok: err == nil, Error: errString(err)})
	case CmdAttach:
		emit := func(event json.RawMessage) error {
			frame, err := encodeJSONLine(StreamEvent{Event: event})
			if err != nil {
				return err
			}
			// Per-frame write deadline: a slow/vanished Attach client applying
			// backpressure must not block the emitter (and, via it, the
			// supervisor) indefinitely — a timed-out write returns an error that
			// ends the subscription instead.
			_ = conn.SetWriteDeadline(time.Now().Add(responseWriteTimeout))
			_, werr := conn.Write(frame)
			return werr
		}
		// Detect the Attach client DISCONNECTING. HandleAttach blocks streaming
		// (today it blocks on ctx.Done since the event surface emits nothing),
		// and Attach is server→client only — the server never reads the conn
		// again after the request line. So without this watcher a client that
		// closes its socket is invisible: the handler blocks forever, leaking its
		// goroutine + wg slot + fd for the life of the supervisor. Read from the
		// conn on a goroutine: any result (EOF on client close, or any error)
		// means the client is gone — cancel ctx so HandleAttach returns. A client
		// that (protocol violation) sends bytes on an Attach conn is also treated
		// as done, which is correct: there is no client→server Attach message.
		attachCtx, attachCancel := context.WithCancel(ctx)
		defer attachCancel()
		go func() {
			// One blocking read is enough to detect the client going away: EOF on
			// a clean close, an error on a reset, or (protocol violation) an
			// unexpected inbound byte — Attach is server→client only, so ANY of
			// these means the subscription should end. No read deadline: this
			// blocks until the client acts; when the handler returns for another
			// reason, handleConn's deferred conn.Close() unblocks this read so the
			// watcher goroutine is reaped too.
			var buf [1]byte
			_, _ = conn.Read(buf[:])
			attachCancel()
		}()
		attachErr := s.handler.HandleAttach(attachCtx, emit)
		// Final frame: single Response signals end-of-stream.
		s.writeResponse(conn, Response{Ok: attachErr == nil, Error: errString(attachErr)})

	case CmdPlanImport, CmdPlanSetStatus, CmdTaskApprove, CmdWorkerKill:
		s.dispatchDrive(ctx, conn, req)

	default:
		s.writeResponse(conn, Response{
			Ok:    false,
			Error: fmt.Sprintf("unknown command: %q", req.Cmd),
			Code:  CodeUnsupportedCommand,
		})
	}
}

// dispatchDrive routes a v2 drive command. If the handler does not implement
// DriveHandler, the command is answered with an unsupported_command response
// so an older supervisor fails cleanly against a newer client.
func (s *Server) dispatchDrive(ctx context.Context, conn net.Conn, req Request) {
	// Reject a client asking for a wire version newer than this supervisor
	// speaks. A future v3 may reuse a v2 command name with changed payload
	// semantics; without this guard we would decode it as v2 and act on it,
	// defeating the versioning contract. An unknown-newer version fails clean
	// with unsupported_command (the same class an old supervisor returns to a
	// newer client), so the client can fall back or report a mismatch. A zero/
	// omitted version is pre-versioned and allowed through.
	if req.ProtoVersion > ProtoVersion {
		s.writeResponse(conn, Response{
			Ok:    false,
			Error: fmt.Sprintf("protocol version %d not supported (this supervisor speaks v%d)", req.ProtoVersion, ProtoVersion),
			Code:  CodeUnsupportedCommand,
		})
		return
	}

	dh, ok := s.handler.(DriveHandler)
	if !ok {
		s.writeResponse(conn, Response{
			Ok:    false,
			Error: fmt.Sprintf("drive command %q not supported by this supervisor", req.Cmd),
			Code:  CodeUnsupportedCommand,
		})
		return
	}

	switch req.Cmd {
	case CmdPlanImport:
		var args PlanImportArgs
		if !s.decodeArgs(conn, req.Args, &args) {
			return
		}
		reply, err := dh.HandlePlanImport(ctx, args)
		s.writeResult(conn, reply, err)
	case CmdPlanSetStatus:
		var args PlanSetStatusArgs
		if !s.decodeArgs(conn, req.Args, &args) {
			return
		}
		reply, err := dh.HandlePlanSetStatus(ctx, args)
		s.writeResult(conn, reply, err)
	case CmdTaskApprove:
		var args TaskApproveArgs
		if !s.decodeArgs(conn, req.Args, &args) {
			return
		}
		err := dh.HandleTaskApprove(ctx, args)
		s.writeResult(conn, OKReply{OK: err == nil}, err)
	case CmdWorkerKill:
		var args WorkerKillArgs
		if !s.decodeArgs(conn, req.Args, &args) {
			return
		}
		err := dh.HandleWorkerKill(ctx, args)
		s.writeResult(conn, OKReply{OK: err == nil}, err)
	}
}

// decodeArgs unmarshals req.Args into dst, writing an invalid_args response
// and returning false on a decode failure. Empty args decode to the zero
// value (some commands accept all-optional args).
func (s *Server) decodeArgs(conn net.Conn, raw json.RawMessage, dst any) bool {
	if len(raw) == 0 {
		return true
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		s.writeResponse(conn, Response{
			Ok:    false,
			Error: fmt.Sprintf("decode args: %v", err),
			Code:  CodeInvalidArgs,
		})
		return false
	}
	return true
}

// Coded is implemented by handler errors that carry a stable machine-readable
// error class (Code* consts). writeResult copies it into Response.Code so the
// client can branch on the failure kind.
type Coded interface {
	Code() string
}

func (s *Server) writeResult(conn net.Conn, data any, err error) {
	if err != nil {
		resp := Response{Error: err.Error()}
		var c Coded
		if errors.As(err, &c) {
			resp.Code = c.Code()
		}
		s.writeResponse(conn, resp)
		return
	}
	raw, marshErr := json.Marshal(data)
	if marshErr != nil {
		s.writeResponse(conn, Response{Error: marshErr.Error()})
		return
	}
	s.writeResponse(conn, Response{Ok: true, Data: raw})
}

func (s *Server) writeResponse(conn net.Conn, resp Response) {
	frame, err := encodeJSONLine(resp)
	if err != nil {
		s.logger.Error("ipc encode response", "err", err)
		return
	}
	// Bound the write: a client that stops reading (or vanishes) must not park
	// this handler in a blocked conn.Write, holding its wg slot and hanging
	// Stop(). The deadline turns that into a prompt write error instead.
	_ = conn.SetWriteDeadline(time.Now().Add(responseWriteTimeout))
	if _, err := conn.Write(frame); err != nil {
		s.logger.Debug("ipc write response", "err", err)
	}
}

// touchFile bumps a file's mtime, creating it with 0o600 if missing.
// Used for heartbeat liveness signalling.
func touchFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // caller-controlled
	if err != nil {
		return fmt.Errorf("touch %s: %w", path, err)
	}
	_ = f.Close()
	now := time.Now()
	return os.Chtimes(path, now, now)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// SocketAlive reports whether the heartbeat file at path was touched
// within maxAge. Clients (`radioactive_ralph status`) call this before attempting
// a socket connection so they can distinguish "service dead"
// from "service slow to respond."
func SocketAlive(heartbeatPath string, maxAge time.Duration) bool {
	info, err := os.Stat(heartbeatPath)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) <= maxAge
}
