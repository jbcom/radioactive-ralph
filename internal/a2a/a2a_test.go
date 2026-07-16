package a2a

import (
	"encoding/json"
	"testing"
)

func TestMarshalUnmarshalEvidenceRoundTrips(t *testing.T) {
	ev := Evidence{
		Ran:          "go test ./...",
		ExitCode:     0,
		Output:       "ok",
		Diff:         "+added a line",
		FilesChanged: []string{"a.go", "b.go"},
	}
	raw, err := MarshalEvidence(ev)
	if err != nil {
		t.Fatalf("MarshalEvidence: %v", err)
	}
	got, err := UnmarshalEvidence(raw)
	if err != nil {
		t.Fatalf("UnmarshalEvidence: %v", err)
	}
	if got.Ran != ev.Ran || got.ExitCode != ev.ExitCode || got.Output != ev.Output || got.Diff != ev.Diff {
		t.Errorf("round trip mismatch: got %+v, want %+v", got, ev)
	}
	if len(got.FilesChanged) != 2 || got.FilesChanged[0] != "a.go" || got.FilesChanged[1] != "b.go" {
		t.Errorf("FilesChanged round trip mismatch: %+v", got.FilesChanged)
	}
}

func TestUnmarshalEvidenceEmptyStringIsZeroValue(t *testing.T) {
	got, err := UnmarshalEvidence("")
	if err != nil {
		t.Fatalf("UnmarshalEvidence(\"\"): %v", err)
	}
	if got.Ran != "" || got.ExitCode != 0 || got.Output != "" || got.Diff != "" || len(got.FilesChanged) != 0 {
		t.Errorf("got %+v, want zero value", got)
	}
}

func TestNewEvidenceMessageCarriesTaskAndContext(t *testing.T) {
	ev := Evidence{Ran: "go build ./...", ExitCode: 0, Output: "built"}
	msg := NewEvidenceMessage(RoleAgent, "task-1", "plan-1", ev)

	if msg.Role != RoleAgent {
		t.Errorf("Role = %v, want %v", msg.Role, RoleAgent)
	}
	if string(msg.TaskID) != "task-1" {
		t.Errorf("TaskID = %q, want %q", msg.TaskID, "task-1")
	}
	if msg.ContextID != "plan-1" {
		t.Errorf("ContextID = %q, want %q", msg.ContextID, "plan-1")
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("Parts len = %d, want 1", len(msg.Parts))
	}
}

func TestEvidenceFromMessageRoundTripsThroughJSON(t *testing.T) {
	ev := Evidence{Ran: "stat output.txt", ExitCode: 0, Output: "exists", FilesChanged: []string{"output.txt"}}
	msg := NewEvidenceMessage(RoleAgent, "task-2", "plan-2", ev)

	// Round-trip through JSON to simulate reloading a message from
	// a2a_messages.content_json (where Data() would come back as
	// map[string]any rather than the original concrete Evidence).
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var reloaded Message
	if err := json.Unmarshal(raw, &reloaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got, err := EvidenceFromMessage(&reloaded)
	if err != nil {
		t.Fatalf("EvidenceFromMessage: %v", err)
	}
	if got.Ran != ev.Ran || got.Output != ev.Output {
		t.Errorf("got %+v, want %+v", got, ev)
	}
	if len(got.FilesChanged) != 1 || got.FilesChanged[0] != "output.txt" {
		t.Errorf("FilesChanged = %+v, want [output.txt]", got.FilesChanged)
	}
}

func TestEvidenceFromMessageNilMessageErrors(t *testing.T) {
	if _, err := EvidenceFromMessage(nil); err == nil {
		t.Fatal("expected an error for a nil message")
	}
}

func TestTaskStateConstantsAreDistinct(t *testing.T) {
	states := []TaskState{StateSubmitted, StateWorking, StateInputRequired, StateCompleted, StateFailed}
	seen := map[TaskState]bool{}
	for _, s := range states {
		if seen[s] {
			t.Errorf("duplicate TaskState value %q", s)
		}
		seen[s] = true
		if s == "" {
			t.Error("TaskState constant is empty")
		}
	}
}
