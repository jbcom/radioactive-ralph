package main

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/vconfig"
)

// TestSupervisorConfigSourceIsTheSameSeamTheSupervisorUses is the architecture
// assertion: the CLI resolves config through the SAME vconfig.ConfigSource the
// supervisor does, so there is one resolution path rather than a client path
// and a server path that drift apart.
func TestSupervisorConfigSourceIsTheSameSeamTheSupervisorUses(t *testing.T) {
	// Passing the CLI's source where a vconfig.ConfigSource is required is the
	// assertion: it compiles only while the CLI and the supervisor share one
	// seam. requireConfigSource exists so the check is a real call rather than
	// a declaration a linter reads as redundant.
	if src := requireConfigSource(newSupervisorConfigSource(nil)); src == nil {
		t.Fatal("newSupervisorConfigSource returned nil")
	}
}

// TestSupervisorConfigSourceCachesTheUserScopeLookup pins a real behavior, not
// just a type. Resolving the user-scope project is a round trip that may CREATE
// the project, so repeating it per config layer would be both slower and
// needlessly write-y — and the values that round trip already returned would be
// fetched a second time for nothing.
func TestSupervisorConfigSourceCachesTheUserScopeLookup(t *testing.T) {
	src := &supervisorConfigSource{
		userProjectID: "cached-user-scope",
		userValues:    map[string]string{"provider": `"claude"`},
	}

	// A nil client would panic on any round trip, so reaching these without
	// error proves the cache was used.
	id, err := src.UserScopeProject(context.Background())
	if err != nil {
		t.Fatalf("UserScopeProject: %v", err)
	}
	if id != "cached-user-scope" {
		t.Fatalf("id = %q, want the cached id", id)
	}

	values, err := src.ProjectConfigValues(context.Background(), id)
	if err != nil {
		t.Fatalf("ProjectConfigValues: %v", err)
	}
	if values["provider"] != `"claude"` {
		t.Fatalf("values = %+v, want the values the user-scope round trip returned", values)
	}
}

// TestSupervisorConfigSourceSkipsAnEmptyApply avoids a pointless write round
// trip when vconfig computed no changes — the common case for `init` on an
// already-configured project.
func TestSupervisorConfigSourceSkipsAnEmptyApply(t *testing.T) {
	src := &supervisorConfigSource{} // nil client: any round trip panics
	if err := src.ApplyProjectConfigValues(context.Background(), "p", nil, nil); err != nil {
		t.Fatalf("empty apply: %v", err)
	}
}

// requireConfigSource accepts anything satisfying the shared seam and hands it
// back, so callers can assert conformance with a real call.
func requireConfigSource(src vconfig.ConfigSource) vconfig.ConfigSource { return src }
