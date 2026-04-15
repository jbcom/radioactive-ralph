package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/plandag"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestPlanListOverInMemoryTransport drives the MCP server with an
// in-memory transport pair, calls the plan.list tool, and verifies
// the response shape. Proves the full client→server→handler→
// plandag→server→client round trip works.
func TestPlanListOverInMemoryTransport(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer store.Close()

	// Seed a plan so plan.list has something to return.
	planID, err := store.CreatePlan(ctx, plandag.CreatePlanOpts{
		Slug: "smoke", Title: "Smoke test plan",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := store.SetPlanStatus(ctx, planID, plandag.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}

	// Need a session row so the tool surface has somewhere to
	// attribute calls.
	sessID, err := store.CreateSession(ctx, plandag.SessionOpts{
		Mode: plandag.SessionModePortable, Transport: plandag.SessionTransportStdio,
		PID: 1, PIDStartTime: "test",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	server, err := New(Options{
		Store:     store,
		SessionID: sessID,
		// No pool — variant.* tools won't register. Plan.* tools
		// don't need it.
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	// Wire client + server with in-memory transports.
	t1, t2 := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.MCPServer().Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	defer serverSession.Close()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0"}, nil)
	clientSession, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer clientSession.Close()

	// Call plan.list via the client.
	res, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "plan.list",
		Arguments: map[string]any{"include_archived": false},
	})
	if err != nil {
		t.Fatalf("CallTool plan.list: %v", err)
	}
	if res.IsError {
		t.Fatalf("plan.list returned error: %#v", res.Content)
	}

	// Decode the structured content into our result shape.
	if res.StructuredContent == nil {
		t.Fatal("plan.list returned no StructuredContent")
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	var got planListResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, raw)
	}

	if len(got.Plans) != 1 {
		t.Fatalf("plans returned = %d, want 1\nfull response: %s", len(got.Plans), raw)
	}
	if got.Plans[0].Slug != "smoke" {
		t.Errorf("plan slug = %q, want smoke", got.Plans[0].Slug)
	}
	if got.Plans[0].Status != "active" {
		t.Errorf("plan status = %q, want active", got.Plans[0].Status)
	}
}

// openTestStore is the shared store-helper for MCP tests.
func openTestStore(t *testing.T, ctx context.Context) *plandag.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "plans.db") +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	s, err := plandag.Open(ctx, plandag.Options{DSN: dsn})
	if err != nil {
		t.Fatalf("plandag.Open: %v", err)
	}
	return s
}
