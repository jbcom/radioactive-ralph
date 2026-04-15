package mcp

import (
	"context"

	"github.com/jbcom/radioactive-ralph/internal/variantpool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── variant.* tools — control of subprocess Ralph instances ──

type variantSpawnArgs struct {
	VariantName string `json:"variant_name"`
	WorkingDir  string `json:"working_dir,omitempty"`
}

type variantSpawnResult struct {
	VariantID string `json:"variant_id"`
	PID       int    `json:"pid"`
}

type variantSayArgs struct {
	VariantID string `json:"variant_id"`
	Message   string `json:"message"`
}

type variantSayResult struct {
	OK bool `json:"ok"`
}

type variantStatusArgs struct {
	VariantID string `json:"variant_id"`
}

type variantStatusResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	PID     int    `json:"pid"`
	Running bool   `json:"running"`
}

type variantKillArgs struct {
	VariantID string `json:"variant_id"`
}

type variantKillResult struct {
	OK bool `json:"ok"`
}

func (s *Server) registerVariantTools() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "variant.spawn",
		Description: "Spawn a managed Ralph subprocess for the named variant. Returns a variant_id used to address the running process.",
	}, s.handleVariantSpawn)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "variant.say",
		Description: "Send a message to a running variant's stdin. The variant interprets it however that variant interprets operator input.",
	}, s.handleVariantSay)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "variant.status",
		Description: "Inspect a running variant — pid, running flag, name.",
	}, s.handleVariantStatus)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "variant.kill",
		Description: "Stop a running variant. Closes the lifeline pipe, then SIGTERM, then SIGKILL after grace.",
	}, s.handleVariantKill)
}

func (s *Server) handleVariantSpawn(ctx context.Context, _ *mcpsdk.CallToolRequest, args variantSpawnArgs) (*mcpsdk.CallToolResult, variantSpawnResult, error) {
	v, err := s.opts.Pool.Spawn(ctx, variantpool.SpawnOpts{
		VariantName: args.VariantName,
		RalphBin:    ralphBinPath(),
		WorkingDir:  args.WorkingDir,
	})
	if err != nil {
		return nil, variantSpawnResult{}, err
	}
	st := v.Status()
	return &mcpsdk.CallToolResult{}, variantSpawnResult{
		VariantID: v.ID,
		PID:       st.PID,
	}, nil
}

func (s *Server) handleVariantSay(_ context.Context, _ *mcpsdk.CallToolRequest, args variantSayArgs) (*mcpsdk.CallToolResult, variantSayResult, error) {
	v := s.opts.Pool.Get(args.VariantID)
	if v == nil {
		return nil, variantSayResult{}, errVariantUnknown(args.VariantID)
	}
	if err := v.Say(args.Message); err != nil {
		return nil, variantSayResult{}, err
	}
	return &mcpsdk.CallToolResult{}, variantSayResult{OK: true}, nil
}

func (s *Server) handleVariantStatus(_ context.Context, _ *mcpsdk.CallToolRequest, args variantStatusArgs) (*mcpsdk.CallToolResult, variantStatusResult, error) {
	v := s.opts.Pool.Get(args.VariantID)
	if v == nil {
		return nil, variantStatusResult{}, errVariantUnknown(args.VariantID)
	}
	st := v.Status()
	return &mcpsdk.CallToolResult{}, variantStatusResult{
		ID: st.ID, Name: st.Name, PID: st.PID, Running: st.Running,
	}, nil
}

func (s *Server) handleVariantKill(ctx context.Context, _ *mcpsdk.CallToolRequest, args variantKillArgs) (*mcpsdk.CallToolResult, variantKillResult, error) {
	v := s.opts.Pool.Get(args.VariantID)
	if v == nil {
		return nil, variantKillResult{}, errVariantUnknown(args.VariantID)
	}
	if err := v.Kill(ctx); err != nil {
		return nil, variantKillResult{}, err
	}
	return &mcpsdk.CallToolResult{}, variantKillResult{OK: true}, nil
}
