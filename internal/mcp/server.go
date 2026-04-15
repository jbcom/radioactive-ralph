// Package mcp wraps modelcontextprotocol/go-sdk to expose Ralph's
// plan-DAG and variant-pool surface as MCP tools. The outer Claude
// session orchestrates ralphs by calling these tools.
//
// Two transports:
//   - Stdio: portable mode. Spawned as a subprocess of the outer
//     Claude session via a Skill.
//   - HTTP+SSE: durable mode. Long-running server bound to a port,
//     installed via brew services / launchd / systemd.
//
// Both transports expose the same tool surface — server.go owns
// the tool registry; transports are thin wrappers in stdio.go and
// http.go.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/plandag"
	"github.com/jbcom/radioactive-ralph/internal/variantpool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options configures Server creation.
type Options struct {
	// Store is the plandag handle backing every plan.* tool.
	Store *plandag.Store

	// Pool is the variantpool handle backing every variant.* tool.
	Pool *variantpool.Pool

	// SessionID is the plandag session this server runs under. Both
	// the plandag CRUD tools and the variantpool tools are scoped
	// to this session.
	SessionID string

	// ServerInfo identifies us in the MCP handshake.
	ServerInfo *mcpsdk.Implementation
}

// Server bundles the registered MCP server with its options.
type Server struct {
	mcp  *mcpsdk.Server
	opts Options
}

// New builds an MCP server with every Ralph tool registered.
func New(o Options) (*Server, error) {
	if o.Store == nil {
		return nil, errors.New("mcp: Store required")
	}
	if o.SessionID == "" {
		return nil, errors.New("mcp: SessionID required")
	}
	if o.ServerInfo == nil {
		o.ServerInfo = &mcpsdk.Implementation{
			Name:    "radioactive_ralph",
			Version: "dev",
		}
	}

	s := mcpsdk.NewServer(o.ServerInfo, nil)
	srv := &Server{mcp: s, opts: o}
	srv.registerPlanTools()
	if o.Pool != nil {
		srv.registerVariantTools()
	}
	return srv, nil
}

// MCPServer returns the underlying SDK server. Useful for tests
// that drive the server via in-memory transports.
func (s *Server) MCPServer() *mcpsdk.Server { return s.mcp }

// ── Plan tools ──────────────────────────────────────────────────

type planListArgs struct {
	IncludeArchived bool `json:"include_archived,omitempty"`
}

type planListResult struct {
	Plans []planListEntry `json:"plans"`
}

type planListEntry struct {
	ID             string `json:"id"`
	Slug           string `json:"slug"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	PrimaryVariant string `json:"primary_variant"`
	Confidence     int    `json:"confidence"`
}

type planShowArgs struct {
	PlanID string `json:"plan_id"`
}

type planShowResult struct {
	Plan       planListEntry `json:"plan"`
	ReadyTasks []taskBrief   `json:"ready_tasks"`
	TotalTasks int           `json:"total_tasks"`
	DoneTasks  int           `json:"done_tasks"`
}

type taskBrief struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	VariantHint string `json:"variant_hint,omitempty"`
	Effort      string `json:"effort,omitempty"`
	Complexity  string `json:"complexity,omitempty"`
}

type planNextArgs struct {
	PlanID  string `json:"plan_id"`
	Variant string `json:"variant,omitempty"`
}

type planClaimArgs struct {
	PlanID           string `json:"plan_id"`
	Variant          string `json:"variant"`
	SessionVariantID string `json:"session_variant_id"`
}

type planMarkDoneArgs struct {
	PlanID       string          `json:"plan_id"`
	TaskID       string          `json:"task_id"`
	EvidenceJSON json.RawMessage `json:"evidence,omitempty"`
}

type planMarkFailedArgs struct {
	PlanID     string `json:"plan_id"`
	TaskID     string `json:"task_id"`
	Reason     string `json:"reason"`
	MaxRetries int    `json:"max_retries,omitempty"`
}

func (s *Server) registerPlanTools() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "plan.list",
		Description: "List all plans known to this radioactive-ralph instance.",
	}, s.handlePlanList)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "plan.show",
		Description: "Show one plan with its ready task set.",
	}, s.handlePlanShow)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "plan.next",
		Description: "Return the next ready task for a plan WITHOUT claiming it. Use plan.claim when you actually intend to start work.",
	}, s.handlePlanNext)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "plan.claim",
		Description: "Atomically claim the next ready task for the given variant. The task transitions to status='running' and is reserved for this session+variant.",
	}, s.handlePlanClaim)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "plan.mark_done",
		Description: "Transition a running task to done. Returns the new ready set so the caller can immediately decide what to start next.",
	}, s.handlePlanMarkDone)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "plan.mark_failed",
		Description: "Mark a running task as failed. Bounded retry: if retry_count < max_retries, the task is requeued as 'pending'; otherwise it sticks in 'failed'.",
	}, s.handlePlanMarkFailed)
}

func (s *Server) handlePlanList(ctx context.Context, _ *mcpsdk.CallToolRequest, args planListArgs) (*mcpsdk.CallToolResult, planListResult, error) {
	statuses := []plandag.PlanStatus{
		plandag.PlanStatusActive, plandag.PlanStatusPaused, plandag.PlanStatusDraft,
	}
	if args.IncludeArchived {
		statuses = append(statuses,
			plandag.PlanStatusArchived, plandag.PlanStatusAbandoned,
			plandag.PlanStatusDone, plandag.PlanStatusFailedPartial,
		)
	}
	plans, err := s.opts.Store.ListPlans(ctx, statuses)
	if err != nil {
		return nil, planListResult{}, err
	}
	out := planListResult{Plans: make([]planListEntry, 0, len(plans))}
	for _, p := range plans {
		out.Plans = append(out.Plans, planListEntry{
			ID:             p.ID,
			Slug:           p.Slug,
			Title:          p.Title,
			Status:         string(p.Status),
			PrimaryVariant: p.PrimaryVariant,
			Confidence:     p.Confidence,
		})
	}
	return &mcpsdk.CallToolResult{}, out, nil
}

func (s *Server) handlePlanShow(ctx context.Context, _ *mcpsdk.CallToolRequest, args planShowArgs) (*mcpsdk.CallToolResult, planShowResult, error) {
	plan, err := s.opts.Store.GetPlan(ctx, args.PlanID)
	if err != nil {
		return nil, planShowResult{}, err
	}
	ready, err := s.opts.Store.Ready(ctx, plan.ID)
	if err != nil {
		return nil, planShowResult{}, err
	}

	// Counts come from a single COUNT(*) per status group.
	var total, done int
	if err := s.opts.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*), SUM(CASE WHEN status = 'done' THEN 1 ELSE 0 END)
		 FROM tasks WHERE plan_id = ?`, plan.ID).Scan(&total, &done); err != nil {
		return nil, planShowResult{}, fmt.Errorf("count tasks: %w", err)
	}

	briefs := make([]taskBrief, 0, len(ready))
	for _, t := range ready {
		briefs = append(briefs, taskBrief{
			ID:          t.ID,
			Description: t.Description,
			VariantHint: t.VariantHint,
			Effort:      t.Effort,
			Complexity:  t.Complexity,
		})
	}

	return &mcpsdk.CallToolResult{}, planShowResult{
		Plan: planListEntry{
			ID:             plan.ID,
			Slug:           plan.Slug,
			Title:          plan.Title,
			Status:         string(plan.Status),
			PrimaryVariant: plan.PrimaryVariant,
			Confidence:     plan.Confidence,
		},
		ReadyTasks: briefs,
		TotalTasks: total,
		DoneTasks:  done,
	}, nil
}

type planNextResult struct {
	Task *taskBrief `json:"task,omitempty"`
}

func (s *Server) handlePlanNext(ctx context.Context, _ *mcpsdk.CallToolRequest, args planNextArgs) (*mcpsdk.CallToolResult, planNextResult, error) {
	ready, err := s.opts.Store.Ready(ctx, args.PlanID)
	if err != nil {
		return nil, planNextResult{}, err
	}
	if len(ready) == 0 {
		return &mcpsdk.CallToolResult{}, planNextResult{}, nil
	}
	// Prefer variant-hint match if the caller specified one.
	for _, t := range ready {
		if args.Variant != "" && t.VariantHint == args.Variant {
			return &mcpsdk.CallToolResult{}, planNextResult{Task: &taskBrief{
				ID: t.ID, Description: t.Description,
				VariantHint: t.VariantHint, Effort: t.Effort, Complexity: t.Complexity,
			}}, nil
		}
	}
	t := ready[0]
	return &mcpsdk.CallToolResult{}, planNextResult{Task: &taskBrief{
		ID: t.ID, Description: t.Description,
		VariantHint: t.VariantHint, Effort: t.Effort, Complexity: t.Complexity,
	}}, nil
}

type planClaimResult struct {
	Task *taskBrief `json:"task,omitempty"`
}

func (s *Server) handlePlanClaim(ctx context.Context, _ *mcpsdk.CallToolRequest, args planClaimArgs) (*mcpsdk.CallToolResult, planClaimResult, error) {
	t, err := s.opts.Store.ClaimNextReady(ctx, args.PlanID, args.Variant, s.opts.SessionID, args.SessionVariantID)
	if err != nil {
		return nil, planClaimResult{}, err
	}
	return &mcpsdk.CallToolResult{}, planClaimResult{Task: &taskBrief{
		ID: t.ID, Description: t.Description,
		VariantHint: t.VariantHint, Effort: t.Effort, Complexity: t.Complexity,
	}}, nil
}

type planMarkDoneResult struct {
	NewlyReady []taskBrief `json:"newly_ready"`
}

func (s *Server) handlePlanMarkDone(ctx context.Context, _ *mcpsdk.CallToolRequest, args planMarkDoneArgs) (*mcpsdk.CallToolResult, planMarkDoneResult, error) {
	ready, err := s.opts.Store.MarkDone(ctx, args.PlanID, args.TaskID, s.opts.SessionID, string(args.EvidenceJSON))
	if err != nil {
		return nil, planMarkDoneResult{}, err
	}
	briefs := make([]taskBrief, 0, len(ready))
	for _, t := range ready {
		briefs = append(briefs, taskBrief{
			ID: t.ID, Description: t.Description,
			VariantHint: t.VariantHint, Effort: t.Effort, Complexity: t.Complexity,
		})
	}
	return &mcpsdk.CallToolResult{}, planMarkDoneResult{NewlyReady: briefs}, nil
}

type planMarkFailedResult struct {
	Retried bool `json:"retried"`
}

func (s *Server) handlePlanMarkFailed(ctx context.Context, _ *mcpsdk.CallToolRequest, args planMarkFailedArgs) (*mcpsdk.CallToolResult, planMarkFailedResult, error) {
	maxRetries := args.MaxRetries
	if maxRetries == 0 {
		maxRetries = 2
	}
	retried, err := s.opts.Store.MarkFailed(ctx, args.PlanID, args.TaskID, s.opts.SessionID, args.Reason, maxRetries)
	if err != nil {
		return nil, planMarkFailedResult{}, err
	}
	return &mcpsdk.CallToolResult{}, planMarkFailedResult{Retried: retried}, nil
}
