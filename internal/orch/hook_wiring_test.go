package orch

import (
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

func TestConfigureManagedHooksRequiresExplicitAcceptanceForEveryTask(t *testing.T) {
	explicit := &store.Task{AcceptanceJSON: `{"command":"exit 0"}`}
	judgment := &store.Task{}

	var req provider.Request
	configureManagedHooks(&req, "session", "/tmp/hook.sock", explicit)
	if req.ManagedSessionID != "session" || req.HookEndpoint != "/tmp/hook.sock" {
		t.Fatalf("explicit task was not hook-managed: %+v", req)
	}

	req = provider.Request{}
	configureManagedHooks(&req, "session", "/tmp/hook.sock", explicit, judgment)
	if req.ManagedSessionID != "" || req.HookEndpoint != "" {
		t.Fatalf("mixed fanout was partially hook-managed: %+v", req)
	}
	for _, acceptance := range []string{"{}", `{"dir":"."}`, `{"command":"  "}`, "not-json"} {
		req = provider.Request{}
		configureManagedHooks(&req, "session", "/tmp/hook.sock", &store.Task{AcceptanceJSON: acceptance})
		if req.ManagedSessionID != "" || req.HookEndpoint != "" {
			t.Fatalf("non-mechanical acceptance %q enabled hooks: %+v", acceptance, req)
		}
	}

	req = provider.Request{}
	configureManagedHooks(&req, "session", "", explicit)
	if req.ManagedSessionID != "" {
		t.Fatalf("missing supervisor endpoint enabled hooks: %+v", req)
	}
}
