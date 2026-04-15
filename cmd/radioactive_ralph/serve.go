package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/mcp"
	"github.com/jbcom/radioactive-ralph/internal/plandag"
	"github.com/jbcom/radioactive-ralph/internal/variantpool"
)

// ServeCmd is `radioactive_ralph serve --mcp`.
//
// Claude Code spawns this as a stdio MCP server. One process = one
// Claude session; the binary remains the source of truth and the MCP
// layer is just the structured control plane.
type ServeCmd struct {
	MCP bool `help:"Required. Serve the MCP protocol."`
}

// Run implements the kong-dispatched serve command.
func (c *ServeCmd) Run(rc *runContext) error {
	if !c.MCP {
		return fmt.Errorf("--mcp is required (no other protocols supported)")
	}

	ctx := rc.ctx

	store, err := openPlanStore(ctx)
	if err != nil {
		return fmt.Errorf("open plan store: %w", err)
	}
	defer func() { _ = store.Close() }()

	sessionID, err := store.CreateSession(ctx, plandag.SessionOpts{
		Mode:         plandag.SessionModePortable,
		Transport:    plandag.SessionTransportStdio,
		PID:          os.Getpid(),
		PIDStartTime: pidStartTime(),
		Host:         hostnameOrLocal(),
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer func() {
		// Best-effort cleanup. Skip the ctx if it was canceled —
		// CloseSession does its own retry under a fresh context.
		_ = store.CloseSession(context.Background(), sessionID)
	}()

	pool, err := variantpool.New(variantpool.Options{
		Store:     store,
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("variantpool.New: %w", err)
	}
	defer func() { _ = pool.Close(context.Background()) }()

	server, err := mcp.New(mcp.Options{
		Store:     store,
		Pool:      pool,
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("mcp.New: %w", err)
	}

	if err := server.ServeStdio(ctx); err != nil {
		return fmt.Errorf("serve stdio: %w", err)
	}
	return nil
}

// pidStartTime returns a string identifier for our process start
// time. Used for FK linking and reaper recycling defense. We don't
// need true /proc-grade precision — even a string approximation is
// enough to disambiguate the same PID being reused later.
func pidStartTime() string {
	// time.Now is fine — within one host's clock skew this uniquely
	// identifies our process within the per-pid recycling window.
	return fmt.Sprintf("ralph-%d", os.Getpid())
}

// hostnameOrLocal returns the hostname or "local" as a fallback.
func hostnameOrLocal() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "local"
	}
	return h
}
