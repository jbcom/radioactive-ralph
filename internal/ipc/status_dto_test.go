package ipc

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStatusReplyCarriesNoHostIdentifiers is a content-safety gate on the live
// CmdStatus wire contract.
//
// RepoPath and PID identify the SUPERVISOR HOST, not the work: an absolute
// filesystem path and an OS process id. Neither is needed to render operator
// status — nothing in the TUI, GUI, or CLI read either — and both travel to
// every attached client, so they were pure exposure. ProviderSessionID is the
// same shape of problem one level down: an external provider's session
// identifier, never populated by the supervisor, on a per-worker DTO.
//
// A field nothing populates and nothing reads is not harmless when it sits on
// a wire contract: it invites a future implementer to fill it in, at which
// point the leak becomes real without anyone deciding to add it.
func TestStatusReplyCarriesNoHostIdentifiers(t *testing.T) {
	raw, err := json.Marshal(StatusReply{
		Workers: []WorkerSummary{{WorkerID: "w1", PlanID: "p1", TaskID: "t1"}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, banned := range []string{"repo_path", `"pid"`, "provider_session_id"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("StatusReply wire form still carries %s: %s", banned, raw)
		}
	}
}

// TestStatusReplyStillCarriesOperatorState is the control. Stripping host
// identifiers must not strip what operators actually use — a status reply that
// carries nothing is worse than one that carries too much.
func TestStatusReplyStillCarriesOperatorState(t *testing.T) {
	raw, err := json.Marshal(StatusReply{
		ProtoVersion:  3,
		ActiveWorkers: 2,
		ReadyTasks:    5,
		RunningTasks:  1,
		ActivePlans:   1,
		Workers:       []WorkerSummary{{WorkerID: "w1", PlanID: "p1", TaskID: "t1", Provider: "claude"}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, required := range []string{
		"proto_version", "active_workers", "ready_tasks", "running_tasks",
		"active_plans", "worker_id", "plan_id", "task_id", "provider",
	} {
		if !strings.Contains(string(raw), required) {
			t.Errorf("StatusReply lost %s: %s", required, raw)
		}
	}
}
