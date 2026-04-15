package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServeStdio runs the MCP server over the stdio transport. Blocks
// until the client closes the connection or ctx is canceled.
//
// Used by the portable mode CLI (`radioactive_ralph serve --mcp`).
// One process = one MCP session; the spawning Claude session owns
// the lifetime.
func (s *Server) ServeStdio(ctx context.Context) error {
	transport := &mcpsdk.StdioTransport{}
	session, err := s.mcp.Connect(ctx, transport, nil)
	if err != nil {
		return err
	}
	return session.Wait()
}
