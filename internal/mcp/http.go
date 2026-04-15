package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServeHTTP runs the MCP server over streamable HTTP. Blocks until
// ctx is canceled or the listener errors.
//
// Used by the durable mode service (`radioactive_ralph serve --mcp --http :port`),
// typically managed by launchd/systemd. One process hosts a single
// shared session — every client that connects pipes JSON-RPC over the
// same stream.
func (s *Server) ServeHTTP(ctx context.Context, addr string) error {
	handler := mcpsdk.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcpsdk.Server { return s.mcp },
		nil,
	)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("mcp: listen %s: %w", addr, err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(lis)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("mcp: http serve: %w", err)
	}
}
