package main

import (
	"fmt"
	"os"
	"os/exec"
)

// MCPCmd groups MCP server client-registration helpers. These shell
// out to `claude mcp add/remove/get` rather than editing Claude Code's
// config files directly — the CLI is the supported interface, and
// using it means we stay correct when Claude changes its on-disk
// format.
type MCPCmd struct {
	Register   MCPRegisterCmd   `cmd:"" help:"Register radioactive_ralph as an MCP server with Claude Code."`
	Unregister MCPUnregisterCmd `cmd:"" help:"Remove radioactive_ralph's MCP server registration."`
	Status     MCPStatusCmd     `cmd:"" help:"Show the registered MCP server entry, if any."`
}

// MCPRegisterCmd is `radioactive_ralph mcp register`. It wires the
// current binary into Claude Code as a stdio MCP server.
type MCPRegisterCmd struct {
	Name  string `help:"Registration name." default:"radioactive_ralph"`
	Scope string `help:"Config scope: local, user, project." default:"user" enum:"local,user,project"`
	Bin   string `help:"Path to radioactive_ralph binary. Defaults to argv[0]."`
}

// Run shells out to `claude mcp add`.
func (c *MCPRegisterCmd) Run(_ *runContext) error {
	bin := c.Bin
	if bin == "" {
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve self-path: %w", err)
		}
		bin = self
	}

	args := []string{
		"mcp", "add", "--scope", c.Scope,
		c.Name, "--", bin, "serve", "--mcp",
	}

	cmd := exec.Command("claude", args...) //nolint:gosec // args are operator-chosen + validated by enum tags
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude mcp add: %w", err)
	}
	fmt.Printf("Registered MCP server %q (stdio) at %s scope.\n", c.Name, c.Scope)
	return nil
}

// MCPUnregisterCmd is `radioactive_ralph mcp unregister`.
type MCPUnregisterCmd struct {
	Name  string `help:"Registration name." default:"radioactive_ralph"`
	Scope string `help:"Config scope: local, user, project." default:"user" enum:"local,user,project"`
}

// Run shells out to `claude mcp remove`.
func (c *MCPUnregisterCmd) Run(_ *runContext) error {
	cmd := exec.Command("claude", "mcp", "remove", "--scope", c.Scope, c.Name) //nolint:gosec // args are operator-chosen
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude mcp remove: %w", err)
	}
	return nil
}

// MCPStatusCmd is `radioactive_ralph mcp status`. Shells out to
// `claude mcp get <name>`; exit code 0 = registered, non-zero = not.
type MCPStatusCmd struct {
	Name string `help:"Registration name." default:"radioactive_ralph"`
}

// Run shells out to `claude mcp get`.
func (c *MCPStatusCmd) Run(_ *runContext) error {
	cmd := exec.Command("claude", "mcp", "get", c.Name) //nolint:gosec // args are operator-chosen
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
