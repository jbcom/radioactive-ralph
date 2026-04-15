package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
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

// MCPRegisterCmd is `radioactive_ralph mcp register`. It invokes
// `claude mcp add` with args shaped for the chosen transport.
type MCPRegisterCmd struct {
	Name      string `help:"Registration name." default:"radioactive_ralph"`
	Scope     string `help:"Config scope: local, user, project." default:"user" enum:"local,user,project"`
	Transport string `help:"Transport: stdio (default) or http." default:"stdio" enum:"stdio,http"`
	HTTPAddr  string `help:"For --transport=http, the URL Claude should connect to." default:"http://localhost:7777/mcp"`
	Bin       string `help:"Path to radioactive_ralph binary. Defaults to argv[0]."`
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

	args := []string{"mcp", "add", "--scope", c.Scope}

	switch c.Transport {
	case "stdio":
		// `claude mcp add <name> -- <cmd> <args>...`
		args = append(args, c.Name, "--", bin, "serve", "--mcp")
	case "http":
		// `claude mcp add --transport http <name> <url>`
		args = append(args, "--transport", "http", c.Name, c.HTTPAddr)
	default:
		return fmt.Errorf("unknown transport %q", c.Transport)
	}

	cmd := exec.Command("claude", args...) //nolint:gosec // args are operator-chosen + validated by enum tags
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude mcp add: %w", err)
	}
	fmt.Printf("Registered MCP server %q (%s) at %s scope.\n", c.Name, c.Transport, c.Scope)
	if c.Transport == "http" {
		fmt.Printf("Start the server with: radioactive_ralph serve --mcp --http %s\n",
			addrFromURL(c.HTTPAddr))
	}
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

// addrFromURL turns "http://host:port/path" into "host:port" for the
// --http bind-address flag on `serve`. Minimal parsing — we only use
// it in a print statement, so best-effort is fine.
func addrFromURL(u string) string {
	if parsed, err := url.Parse(u); err == nil && parsed.Host != "" {
		return parsed.Host
	}

	for _, prefix := range []string{"http://", "https://"} {
		if trimmed, ok := strings.CutPrefix(u, prefix); ok {
			u = trimmed
			break
		}
	}
	if host, _, ok := strings.Cut(u, "/"); ok {
		return host
	}
	return u
}
