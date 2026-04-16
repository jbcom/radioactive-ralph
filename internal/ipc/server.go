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

// Server is the repo-service IPC server. One instance per repo service.
type Server struct {
	socketPath    string
	heartbeatPath string
	listener      net.Listener
	handler       Handler
	logger        *slog.Logger
	wg            sync.WaitGroup
	stopCh        chan struct{}
	heartbeatCh   chan struct{}
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
	if opts.HeartbeatInterval == 0 {
		opts.HeartbeatInterval = 10 * time.Second
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		socketPath:    opts.SocketPath,
		heartbeatPath: opts.HeartbeatPath,
		handler:       opts.Handler,
		logger:        logger,
		stopCh:        make(chan struct{}),
		heartbeatCh:   make(chan struct{}),
	}, nil
}

// Start binds the socket and begins accepting connections in a background
// goroutine. Safe to call once. Returns the listener error if bind fails.
func (s *Server) Start(heartbeatInterval time.Duration) error {
	l, err := listenEndpoint(s.socketPath)
	if err != nil {
		return fmt.Errorf("ipc: listen %s: %w", s.socketPath, err)
	}
	s.listener = l

	s.wg.Add(2)
	go s.heartbeatLoop(heartbeatInterval)
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
	s.wg.Wait()
	_ = cleanupEndpoint(s.socketPath)
	return nil
}

func (s *Server) heartbeatLoop(interval time.Duration) {
	defer s.wg.Done()
	if interval == 0 {
		interval = 10 * time.Second
	}
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
			// Accept errors during normal operation: log and continue.
			// The only fatal case is a closed listener, which we already
			// skipped via the select above.
			if !errors.Is(err, net.ErrClosed) {
				s.logger.Warn("ipc accept error", "err", err)
			}
			return
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

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		if !errors.Is(err, io.EOF) {
			s.logger.Debug("ipc read request", "err", err)
		}
		return
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		s.writeResponse(conn, Response{Error: fmt.Sprintf("malformed request: %v", err)})
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
			_, werr := conn.Write(frame)
			return werr
		}
		attachErr := s.handler.HandleAttach(ctx, emit)
		// Final frame: single Response signals end-of-stream.
		s.writeResponse(conn, Response{Ok: attachErr == nil, Error: errString(attachErr)})
	default:
		s.writeResponse(conn, Response{Error: fmt.Sprintf("unknown command: %q", req.Cmd)})
	}
}

func (s *Server) writeResult(conn net.Conn, data any, err error) {
	if err != nil {
		s.writeResponse(conn, Response{Error: err.Error()})
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
