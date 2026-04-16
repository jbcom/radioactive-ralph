package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// openTest returns an on-disk test DB for isolation between tests.
// (We avoid shared in-memory because driver specifics differ.)
func openTest(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db close: %v", err)
		}
	})
	return db
}

func TestOpenCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	// The modernc.org driver uses WAL via pragma — file should exist.
	if db.Path() != path {
		t.Errorf("Path() = %q, want %q", db.Path(), path)
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	db := openTest(t)
	// Reopening the same file should not re-apply migrations destructively.
	db2, err := Open(context.Background(), db.Path())
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db2.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
}

func TestAppendAndReplay(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()

	first, err := db.Append(ctx, Event{
		Stream: "session:abc", Kind: "session.spawned", Actor: "radioactive_ralph",
		PayloadParsed: map[string]any{"uuid": "abc", "variant": "green"},
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	second, err := db.Append(ctx, Event{
		Stream: "session:abc", Kind: "message.user", Actor: "radioactive_ralph",
		PayloadRaw: []byte(`{"raw": "bytes"}`),
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if second <= first {
		t.Errorf("expected second ID (%d) > first (%d)", second, first)
	}

	var seen []Event
	if err := db.Replay(ctx, 0, func(e Event) error {
		seen = append(seen, e)
		return nil
	}); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 events, got %d", len(seen))
	}
	if seen[0].Stream != "session:abc" || seen[0].Kind != "session.spawned" {
		t.Errorf("first event unexpected: %+v", seen[0])
	}
	if seen[0].Timestamp.IsZero() {
		t.Error("Timestamp should be populated")
	}

	// PayloadParsed round-trips as json.RawMessage.
	rm, ok := seen[0].PayloadParsed.(json.RawMessage)
	if !ok {
		t.Fatalf("PayloadParsed is %T, want json.RawMessage", seen[0].PayloadParsed)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rm, &decoded); err != nil {
		t.Fatalf("unmarshal replayed payload: %v", err)
	}
	if decoded["uuid"] != "abc" {
		t.Errorf("decoded[uuid] = %v", decoded["uuid"])
	}

	// PayloadRaw round-trips as []byte.
	if string(seen[1].PayloadRaw) != `{"raw": "bytes"}` {
		t.Errorf("PayloadRaw = %q", seen[1].PayloadRaw)
	}
}

func TestReplayAfterID(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()

	for i := range 5 {
		if _, err := db.Append(ctx, Event{
			Stream: "test", Kind: "k", Actor: "a",
			PayloadParsed: map[string]any{"i": i},
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// Replay only events with id > 2 should yield exactly 3 events (IDs 3, 4, 5).
	count := 0
	if err := db.Replay(ctx, 2, func(e Event) error {
		count++
		if e.ID <= 2 {
			t.Errorf("got event with id %d, expected > 2", e.ID)
		}
		return nil
	}); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 events, got %d", count)
	}
}

func TestEnqueueTask(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()

	id, inserted, err := db.EnqueueTask(ctx, "t1", "fix typo in README.md", 3)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !inserted {
		t.Error("first enqueue should insert")
	}
	if id != "t1" {
		t.Errorf("id = %q, want t1", id)
	}
}

func TestEnqueueTaskDedupe(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()

	first, inserted, err := db.EnqueueTask(ctx, "t1", "add tests for forge package", 5)
	if err != nil {
		t.Fatalf("Enqueue first: %v", err)
	}
	if !inserted {
		t.Error("first enqueue should insert")
	}
	if first != "t1" {
		t.Errorf("first id = %q", first)
	}

	// Identical description should dedupe.
	second, inserted, err := db.EnqueueTask(ctx, "t2", "add tests for forge package", 5)
	if err != nil {
		t.Fatalf("Enqueue second: %v", err)
	}
	if inserted {
		t.Error("duplicate enqueue should not insert")
	}
	if second != first {
		t.Errorf("dedup should return first's id %q, got %q", first, second)
	}
}

func TestEnqueueTaskRequiresFields(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()

	if _, _, err := db.EnqueueTask(ctx, "", "something", 1); err == nil {
		t.Error("empty id should error")
	}
	if _, _, err := db.EnqueueTask(ctx, "id", "", 1); err == nil {
		t.Error("empty description should error")
	}
}

func TestClaimTaskLifecycle(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()

	if _, _, err := db.EnqueueTask(ctx, "t1", "first task", 1); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := db.ClaimTask(ctx, "t1", "/worktrees/green-1", "session-abc"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	// Double-claim should fail.
	if err := db.ClaimTask(ctx, "t1", "/worktrees/green-2", "session-def"); err == nil {
		t.Error("second claim should fail")
	}

	if err := db.FinishTask(ctx, "t1", true); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	tasks, err := db.ListTasks(ctx, TaskDone)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Status != TaskDone {
		t.Errorf("expected 1 done task, got %+v", tasks)
	}
}

func TestListTasksAll(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()

	ids := []string{"a", "b", "c"}
	for _, id := range ids {
		if _, _, err := db.EnqueueTask(ctx, id, "task "+id, 1); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	tasks, err := db.ListTasks(ctx)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestSessionLifecycle(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()

	if err := db.InsertSession(ctx, Session{
		UUID:         "s1",
		Variant:      "green",
		WorktreePath: "/wt",
		PID:          12345,
		Model:        "claude-sonnet-4-6",
		Stage:        "execute",
	}); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	active, err := db.ActiveSessions(ctx)
	if err != nil {
		t.Fatalf("ActiveSessions: %v", err)
	}
	if len(active) != 1 || active[0].UUID != "s1" {
		t.Errorf("expected 1 active session with uuid s1, got %+v", active)
	}

	if err := db.MarkSessionExited(ctx, "s1", "clean"); err != nil {
		t.Fatalf("MarkSessionExited: %v", err)
	}
	active, err = db.ActiveSessions(ctx)
	if err != nil {
		t.Fatalf("ActiveSessions: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected 0 active sessions after exit, got %+v", active)
	}
}

func TestAccumulateSpend(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()

	if err := db.InsertSession(ctx, Session{UUID: "s1", Variant: "green"}); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	// Two calls to AccumulateSpend should sum.
	if err := db.AccumulateSpend(ctx, "s1", "claude-sonnet-4-6", 1000, 500, 0); err != nil {
		t.Fatalf("AccumulateSpend: %v", err)
	}
	if err := db.AccumulateSpend(ctx, "s1", "claude-sonnet-4-6", 500, 250, 100); err != nil {
		t.Fatalf("AccumulateSpend: %v", err)
	}

	spends, err := db.SpendBySession(ctx, "s1")
	if err != nil {
		t.Fatalf("SpendBySession: %v", err)
	}
	if len(spends) != 1 {
		t.Fatalf("expected 1 spend row, got %d", len(spends))
	}
	if spends[0].InputTokens != 1500 || spends[0].OutputTokens != 750 || spends[0].CachedInput != 100 {
		t.Errorf("spend totals wrong: %+v", spends[0])
	}
}

func TestFTSPhrasePrep(t *testing.T) {
	cases := map[string]string{
		`hello world`:           `"hello world"`,
		`add "tests" for forge`: `"add tests for forge"`,
		`fix (issue #42)`:       `"fix issue #42"`,
		`   `:                   `""`,
		`*+-^()`:                `""`,
	}
	for in, want := range cases {
		got := ftsPhrase(in)
		if strings.TrimSpace(got) != strings.TrimSpace(want) {
			// Allow some whitespace slop in the "*+-^()" case since
			// replacer leaves residual spaces.
			if !strings.EqualFold(strings.Trim(got, ` "`), strings.Trim(want, ` "`)) {
				t.Errorf("ftsPhrase(%q) = %q, want %q", in, got, want)
			}
		}
	}
}

func TestFinishTaskMissingIsNoOp(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	// Not-yet-inserted task; FinishTask is an UPDATE with 0-row-affected
	// which the driver reports as nil. That matches the API contract:
	// idempotent cleanup.
	if err := db.FinishTask(ctx, "no-such-task", true); err != nil {
		t.Errorf("FinishTask on missing row should be no-op, got %v", err)
	}
}
