package observe

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

type fakeReader struct {
	snapshot     *store.OperatorSnapshot
	snapshotErr  error
	snapshotCall int
	snapshotQ    store.OperatorSnapshotQuery
	messages     *store.OperatorMessagePage
	messagesErr  error
	descriptions map[string]string
	detailErr    error
	messagesCall int
	messagesQ    store.OperatorMessageQuery
}

func (f *fakeReader) ReadOperatorSnapshot(
	_ context.Context,
	q store.OperatorSnapshotQuery,
) (*store.OperatorSnapshot, error) {
	f.snapshotCall++
	f.snapshotQ = q
	return f.snapshot, f.snapshotErr
}

func (f *fakeReader) ListOperatorMessages(
	_ context.Context,
	q store.OperatorMessageQuery,
) (*store.OperatorMessagePage, error) {
	f.messagesCall++
	f.messagesQ = q
	return f.messages, f.messagesErr
}

func operatorSnapshotFixture() *store.OperatorSnapshot {
	captured := time.Date(2026, time.July, 26, 17, 0, 0, 0, time.UTC)
	updated := captured.Add(-time.Minute)
	return &store.OperatorSnapshot{
		CapturedAt: captured,
		Project: store.OperatorProject{
			ID:          "project-1",
			DisplayName: "Project One",
			CreatedAt:   captured.Add(-time.Hour),
			UpdatedAt:   updated,
		},
		PlanCounts: []store.OperatorStatusCount{
			{Status: string(store.PlanStatusActive), Count: 1},
			{Status: string(store.PlanStatusDone), Count: 2},
		},
		TaskCounts: []store.OperatorStatusCount{
			{Status: string(store.TaskStatusDone), Count: 4},
			{Status: string(store.TaskStatusRunning), Count: 2},
		},
		Plans: store.OperatorPlanPage{
			Items: []store.OperatorPlan{{
				ID:        "plan-1",
				Slug:      "ship-it",
				Title:     "Ship It",
				Status:    store.PlanStatusActive,
				TaskDone:  1,
				TaskTotal: 2,
				CreatedAt: captured.Add(-time.Hour),
				UpdatedAt: updated,
			}},
			HasMore:     true,
			NextAfterID: "plan-1",
		},
		Tasks: store.OperatorTaskPage{
			Items: []store.OperatorTask{{
				PlanID:            "plan-1",
				ID:                "task-1",
				Status:            store.TaskStatusRunning,
				RetryCount:        2,
				ReclaimCount:      1,
				ClaimedByWorkerID: "worker-1",
				CreatedAt:         captured.Add(-30 * time.Minute),
				UpdatedAt:         updated,
			}},
			HasMore: true,
			NextAfter: &store.OperatorTaskCursor{
				PlanID: "plan-1",
				TaskID: "task-1",
			},
		},
		ActiveWorkerCount: 1,
		Workers: []store.OperatorWorker{{
			ID:            "worker-1",
			Provider:      "codex",
			Model:         "gpt-5",
			NativeFanout:  true,
			Status:        "running",
			StartedAt:     captured.Add(-10 * time.Minute),
			LastHeartbeat: captured.Add(-time.Second),
			Claims: []store.OperatorWorkerClaim{
				{PlanID: "plan-1", TaskID: "task-1", Status: store.TaskStatusRunning},
				{PlanID: "plan-1", TaskID: "task-2", Status: store.TaskStatusRunning},
			},
		}},
		EventCursor: 9,
		RecentEvents: store.OperatorEventPage{
			Items: []store.OperatorEvent{
				{
					ID:         9,
					PlanID:     "plan-1",
					TaskID:     "task-1",
					Kind:       "task.failed",
					Stream:     "worker",
					OccurredAt: captured.Add(-2 * time.Minute),
				},
				{
					ID:         8,
					PlanID:     "plan-1",
					TaskID:     "task-1",
					Kind:       "task.progress",
					Stream:     "worker",
					OccurredAt: captured.Add(-3 * time.Minute),
				},
			},
			HasMore:      true,
			NextBeforeID: 8,
		},
	}
}

func TestServiceSnapshotProjectsOneSafeConsistentRead(t *testing.T) {
	reader := &fakeReader{snapshot: operatorSnapshotFixture()}
	service, err := New(reader)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	query := SnapshotQuery{
		ProjectID:     "project-1",
		PlanLimit:     10,
		PlanAfterID:   "prior-plan",
		TaskLimit:     11,
		TaskAfter:     TaskCursor{PlanID: "prior-plan", TaskID: "prior-task"},
		EventLimit:    12,
		EventBeforeID: 13,
	}
	got, err := service.Snapshot(context.Background(), query)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if reader.snapshotCall != 1 {
		t.Fatalf("snapshot reader calls = %d, want exactly one", reader.snapshotCall)
	}
	wantStoreQuery := store.OperatorSnapshotQuery{
		ProjectID:     query.ProjectID,
		PlanLimit:     query.PlanLimit,
		PlanAfterID:   query.PlanAfterID,
		TaskLimit:     query.TaskLimit,
		TaskAfter:     store.OperatorTaskCursor(query.TaskAfter),
		EventLimit:    query.EventLimit,
		EventBeforeID: query.EventBeforeID,
	}
	if !reflect.DeepEqual(reader.snapshotQ, wantStoreQuery) {
		t.Fatalf("store query = %+v, want %+v", reader.snapshotQ, wantStoreQuery)
	}
	if got.SchemaVersion != SchemaVersion ||
		got.CapturedAt != reader.snapshot.CapturedAt ||
		got.Project.ID != "project-1" ||
		got.EventCursor != 9 {
		t.Fatalf("snapshot envelope = %+v", got)
	}
	if got.Summary.PlanTotal != 3 || got.Summary.TaskTotal != 6 ||
		got.Summary.ActiveWorkerCount != 1 || got.Summary.ZeroActiveWorkers {
		t.Fatalf("summary = %+v, want complete nonzero-worker totals", got.Summary)
	}
	if len(got.Workers) != 1 || len(got.Workers[0].Claims) != 2 {
		t.Fatalf("workers = %+v, want one fanout worker and both claims", got.Workers)
	}
	if !got.Plans.HasMore || got.Plans.NextAfterID != "plan-1" ||
		!got.Tasks.HasMore || got.Tasks.NextAfter == nil ||
		!got.RecentEvents.HasMore || got.RecentEvents.NextBeforeID != 8 {
		t.Fatalf(
			"page cursors were not preserved: plans=%+v tasks=%+v events=%+v",
			got.Plans,
			got.Tasks,
			got.RecentEvents,
		)
	}
	if got.Plans.Items[0].TaskDone != 1 ||
		got.Plans.Items[0].TaskTotal != 2 ||
		got.Plans.Items[0].Status != string(store.PlanStatusActive) {
		t.Fatalf("safe plan progress/status = %+v", got.Plans.Items[0])
	}
	if got.Tasks.Items[0].Status != string(store.TaskStatusRunning) ||
		got.Workers[0].Claims[0].Status != string(store.TaskStatusRunning) {
		t.Fatalf(
			"transport-neutral statuses = task %q claim %q",
			got.Tasks.Items[0].Status,
			got.Workers[0].Claims[0].Status,
		)
	}

	task := got.Tasks.Items[0]
	if task.CanonicalID != "plan-1:task-1" || task.A2ATask == nil {
		t.Fatalf("task identity/A2A projection = %+v", task)
	}
	if task.A2ATask.ID != sdka2a.TaskID(task.CanonicalID) ||
		task.A2ATask.ContextID != "plan-1" ||
		task.A2ATask.Status.State != sdka2a.TaskStateWorking ||
		task.A2ATask.Status.Timestamp == nil ||
		*task.A2ATask.Status.Timestamp != task.UpdatedAt {
		t.Fatalf("official A2A Task = %+v", task.A2ATask)
	}
	if task.A2ATask.History != nil || task.A2ATask.Artifacts != nil ||
		task.A2ATask.Status.Message != nil {
		t.Fatalf("A2A Task included content-bearing fields: %+v", task.A2ATask)
	}
	namespace, ok := task.A2ATask.Metadata[MetadataKey].(map[string]any)
	if !ok {
		t.Fatalf("A2A metadata namespace = %#v", task.A2ATask.Metadata)
	}
	if namespace["plan_id"] != "plan-1" ||
		namespace["task_id"] != "task-1" ||
		namespace["ralph_status"] != string(store.TaskStatusRunning) ||
		namespace["retry_count"] != 2 ||
		namespace["reclaim_count"] != 1 {
		t.Fatalf("Ralph A2A metadata = %#v", namespace)
	}
	if got.RecentEvents.Items[0].Failure == nil ||
		got.RecentEvents.Items[0].Failure.Category != FailureTaskAttempt ||
		!got.RecentEvents.Items[0].Failure.Retryable {
		t.Fatalf("safe failure classification = %+v", got.RecentEvents.Items[0].Failure)
	}
	if got.RecentEvents.Items[1].Failure != nil {
		t.Fatalf("non-failure event received failure summary: %+v", got.RecentEvents.Items[1])
	}
}

func TestServiceSnapshotForwardsDrillDownScope(t *testing.T) {
	raw := operatorSnapshotFixture()
	raw.Plans.HasMore = false
	raw.Plans.NextAfterID = ""
	raw.Tasks.HasMore = false
	raw.Tasks.NextAfter = nil
	raw.RecentEvents.HasMore = false
	raw.RecentEvents.NextBeforeID = 0
	reader := &fakeReader{snapshot: raw}
	service, err := New(reader)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	query := SnapshotQuery{
		ProjectID:  "project-1",
		PlanID:     "plan-1",
		TaskID:     "task-1",
		PlanLimit:  1,
		TaskLimit:  1,
		EventLimit: 20,
	}
	got, err := service.Snapshot(context.Background(), query)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if reader.snapshotQ.PlanID != query.PlanID ||
		reader.snapshotQ.TaskID != query.TaskID {
		t.Fatalf("store scope = %+v, want plan-1/task-1", reader.snapshotQ)
	}
	if err := ValidateSnapshotResponse(got, query); err != nil {
		t.Fatalf("ValidateSnapshotResponse: %v", err)
	}
}

func TestValidateSnapshotResponseFailsClosed(t *testing.T) {
	valid := func() *Snapshot {
		return &Snapshot{
			SchemaVersion: SchemaVersion,
			Project:       Project{ID: "project-1"},
			Summary: Summary{
				ActiveWorkerCount: 0,
				ZeroActiveWorkers: true,
			},
			Plans: PlanPage{Items: []Plan{{
				ID: "plan-1", TaskDone: 1, TaskTotal: 2,
			}}},
			Tasks: TaskPage{Items: []Task{{
				PlanID: "plan-1", ID: "task-1",
			}}},
			Workers:     []Worker{},
			EventCursor: 9,
			RecentEvents: EventPage{Items: []Event{{
				ID: 9, PlanID: "plan-1", TaskID: "task-1",
			}}},
		}
	}
	query := SnapshotQuery{
		ProjectID: "project-1",
		PlanID:    "plan-1",
		TaskID:    "task-1",
	}
	tests := []struct {
		name   string
		mutate func(*Snapshot)
		want   error
	}{
		{
			name: "schema",
			mutate: func(snapshot *Snapshot) {
				snapshot.SchemaVersion++
			},
			want: ErrIncompatibleSchema,
		},
		{
			name: "project",
			mutate: func(snapshot *Snapshot) {
				snapshot.Project.ID = "other"
			},
			want: ErrInvalidResponse,
		},
		{
			name: "nil collection",
			mutate: func(snapshot *Snapshot) {
				snapshot.Tasks.Items = nil
			},
			want: ErrInvalidResponse,
		},
		{
			name: "worker count",
			mutate: func(snapshot *Snapshot) {
				snapshot.Summary.ActiveWorkerCount = 1
			},
			want: ErrInvalidResponse,
		},
		{
			name: "progress",
			mutate: func(snapshot *Snapshot) {
				snapshot.Plans.Items[0].TaskDone = 3
			},
			want: ErrInvalidResponse,
		},
		{
			name: "task scope",
			mutate: func(snapshot *Snapshot) {
				snapshot.Tasks.Items[0].ID = "other"
			},
			want: ErrInvalidResponse,
		},
		{
			name: "event cursor",
			mutate: func(snapshot *Snapshot) {
				snapshot.EventCursor = 8
			},
			want: ErrInvalidResponse,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := valid()
			test.mutate(snapshot)
			if err := ValidateSnapshotResponse(snapshot, query); !errors.Is(err, test.want) {
				t.Fatalf("ValidateSnapshotResponse error = %v, want %v", err, test.want)
			}
		})
	}
	if err := ValidateSnapshotResponse(valid(), query); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
}

func TestValidateMessageResponseFailsClosed(t *testing.T) {
	valid := func() *MessagePage {
		return &MessagePage{
			SchemaVersion: SchemaVersion,
			Items: []MessageMetadata{
				{
					ID:              1,
					PlanID:          "plan-1",
					TaskID:          "task-1",
					CanonicalTaskID: "plan-1:task-1",
					ContextID:       "plan-1",
					Role:            sdka2a.MessageRoleUser,
				},
				{
					ID:              2,
					PlanID:          "plan-1",
					TaskID:          "task-1",
					CanonicalTaskID: "plan-1:task-1",
					ContextID:       "plan-1",
					Role:            sdka2a.MessageRoleAgent,
				},
			},
			HasMore:     true,
			NextAfterID: 2,
		}
	}
	tests := []struct {
		name   string
		mutate func(*MessagePage)
		want   error
	}{
		{
			name: "schema",
			mutate: func(page *MessagePage) {
				page.SchemaVersion++
			},
			want: ErrIncompatibleSchema,
		},
		{
			name: "nil collection",
			mutate: func(page *MessagePage) {
				page.Items = nil
			},
			want: ErrInvalidResponse,
		},
		{
			name: "cursor",
			mutate: func(page *MessagePage) {
				page.NextAfterID = 0
			},
			want: ErrInvalidResponse,
		},
		{
			name: "identity",
			mutate: func(page *MessagePage) {
				page.Items[0].CanonicalTaskID = "other"
			},
			want: ErrInvalidResponse,
		},
		{
			name: "role",
			mutate: func(page *MessagePage) {
				page.Items[0].Role = sdka2a.MessageRole("ROLE_FUTURE")
			},
			want: ErrInvalidResponse,
		},
		{
			name: "order",
			mutate: func(page *MessagePage) {
				page.Items[1].ID = 1
			},
			want: ErrInvalidResponse,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page := valid()
			test.mutate(page)
			if err := ValidateMessageResponse(page); !errors.Is(err, test.want) {
				t.Fatalf("ValidateMessageResponse error = %v, want %v", err, test.want)
			}
		})
	}
	if err := ValidateMessageResponse(valid()); err != nil {
		t.Fatalf("valid message page rejected: %v", err)
	}
}

func TestSnapshotJSONIsContentFreeAndMakesNoNetworkAgentClaim(t *testing.T) {
	reader := &fakeReader{snapshot: operatorSnapshotFixture()}
	service, err := New(reader)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := service.Snapshot(
		context.Background(),
		SnapshotQuery{ProjectID: "project-1"},
	)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	text := string(encoded)
	for _, forbidden := range []string{
		`"description"`,
		`"source_markdown"`,
		`"acceptance_json"`,
		`"payload"`,
		`"actor"`,
		`"history"`,
		`"artifacts"`,
		`"parts"`,
		`"message"`,
		`"session_id"`,
		`"provider_session_id"`,
		`"subprocess_pid"`,
		`"fingerprint"`,
		`"repository_path"`,
		`"config"`,
		`"environment"`,
		`"agentCard"`,
		`"agent_card"`,
		`"capabilities"`,
		`"interfaces"`,
		`"url"`,
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("snapshot JSON contains forbidden content or capability field %s: %s", forbidden, text)
		}
	}
}

func TestStateForTaskUsesOfficialLifecycleAndRejectsUnknown(t *testing.T) {
	tests := []struct {
		status store.TaskStatus
		want   sdka2a.TaskState
	}{
		{store.TaskStatusPending, sdka2a.TaskStateSubmitted},
		{store.TaskStatusReady, sdka2a.TaskStateSubmitted},
		{store.TaskStatusReadyPendingApproval, sdka2a.TaskStateInputRequired},
		{store.TaskStatusBlocked, sdka2a.TaskStateInputRequired},
		{store.TaskStatusRunning, sdka2a.TaskStateWorking},
		{store.TaskStatusDone, sdka2a.TaskStateCompleted},
		{store.TaskStatusDecomposed, sdka2a.TaskStateCompleted},
		{store.TaskStatusFailed, sdka2a.TaskStateFailed},
		{store.TaskStatusSkipped, sdka2a.TaskStateCanceled},
	}
	for _, test := range tests {
		got, err := StateForTask(test.status)
		if err != nil {
			t.Fatalf("StateForTask(%q): %v", test.status, err)
		}
		if got != test.want {
			t.Errorf("StateForTask(%q) = %q, want %q", test.status, got, test.want)
		}
	}
	got, err := StateForTask(store.TaskStatus("future-unknown"))
	if got != sdka2a.TaskStateUnspecified || !errors.Is(err, ErrUnknownTaskStatus) {
		t.Fatalf("unknown StateForTask = (%q, %v), want unspecified ErrUnknownTaskStatus", got, err)
	}
}

func TestFailureForEventUsesOnlyClosedStaticTaxonomy(t *testing.T) {
	tests := []struct {
		kind      string
		category  FailureCategory
		summary   string
		retryable bool
	}{
		{"task.failed", FailureTaskAttempt, "task attempt failed and was requeued", true},
		{"task.failed_terminal", FailureTaskTerminal, "task retry budget was exhausted", false},
		{"worker.verification_failed", FailureVerification, "completion evidence failed verification", true},
		{"worker.dispatch_error", FailureDispatch, "worker dispatch failed", true},
		{"worker.dispatch_panic", FailureDispatch, "worker dispatch failed unexpectedly", true},
		{"worker.admission_refused", FailureAdmission, "worker admission was refused", false},
	}
	for _, test := range tests {
		got := failureForEvent(test.kind)
		if got == nil {
			t.Fatalf("failureForEvent(%q) = nil", test.kind)
		}
		if got.Category != test.category ||
			got.Summary != test.summary ||
			got.Retryable != test.retryable {
			t.Errorf("failureForEvent(%q) = %+v, want %+v", test.kind, got, test)
		}
	}
	if got := failureForEvent("worker.private-output"); got != nil {
		t.Fatalf("unknown event was classified from content: %+v", got)
	}
}

func TestServiceMessagesProjectsBoundedContentFreeMetadata(t *testing.T) {
	occurred := time.Date(2026, time.July, 26, 17, 15, 0, 0, time.UTC)
	reader := &fakeReader{
		messages: &store.OperatorMessagePage{
			Items: []store.OperatorMessageMetadata{
				{
					ID:         10,
					WorkerID:   "worker-1",
					PlanID:     "plan-1",
					TaskID:     "task-1",
					Role:       string(sdka2a.MessageRoleAgent),
					OccurredAt: occurred,
				},
				{
					ID:         11,
					PlanID:     "plan-1",
					TaskID:     "task-1",
					Role:       string(sdka2a.MessageRoleUser),
					OccurredAt: occurred.Add(time.Second),
				},
			},
			HasMore:     true,
			NextAfterID: 11,
		},
	}
	service, err := New(reader)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	query := MessageQuery{
		ProjectID: "project-1",
		PlanID:    "plan-1",
		TaskID:    "task-1",
		AfterID:   9,
		Limit:     2,
	}
	got, err := service.Messages(context.Background(), query)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if reader.messagesCall != 1 {
		t.Fatalf("message reader calls = %d, want exactly one", reader.messagesCall)
	}
	wantStoreQuery := store.OperatorMessageQuery{
		ProjectID: query.ProjectID,
		PlanID:    query.PlanID,
		TaskID:    query.TaskID,
		AfterID:   query.AfterID,
		Limit:     query.Limit,
	}
	if !reflect.DeepEqual(reader.messagesQ, wantStoreQuery) {
		t.Fatalf("store message query = %+v, want %+v", reader.messagesQ, wantStoreQuery)
	}
	if got.SchemaVersion != SchemaVersion || len(got.Items) != 2 ||
		!got.HasMore || got.NextAfterID != 11 {
		t.Fatalf("message page = %+v", got)
	}
	if got.Items[0].Role != sdka2a.MessageRoleAgent ||
		got.Items[1].Role != sdka2a.MessageRoleUser ||
		got.Items[0].CanonicalTaskID != "plan-1:task-1" ||
		got.Items[0].ContextID != "plan-1" {
		t.Fatalf("message metadata semantics = %+v", got.Items)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal message page: %v", err)
	}
	for _, forbidden := range []string{
		`"content"`,
		`"content_json"`,
		`"parts"`,
		`"text"`,
		`"data"`,
		`"provider_output"`,
		`"session_id"`,
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("message metadata leaked content field %s: %s", forbidden, encoded)
		}
	}
}

func TestServiceFailsClosedOnReaderAndProjectionErrors(t *testing.T) {
	if service, err := New(nil); service != nil || !errors.Is(err, ErrReaderRequired) {
		t.Fatalf("New(nil) = (%+v, %v), want nil ErrReaderRequired", service, err)
	}
	var typedNil *fakeReader
	if service, err := New(typedNil); service != nil || !errors.Is(err, ErrReaderRequired) {
		t.Fatalf("New(typed nil) = (%+v, %v), want nil ErrReaderRequired", service, err)
	}
	var zero Service
	if got, err := zero.Snapshot(context.Background(), SnapshotQuery{}); got != nil ||
		!errors.Is(err, ErrReaderRequired) {
		t.Fatalf("zero Snapshot = (%+v, %v), want nil ErrReaderRequired", got, err)
	}
	if got, err := zero.Messages(context.Background(), MessageQuery{}); got != nil ||
		!errors.Is(err, ErrReaderRequired) {
		t.Fatalf("zero Messages = (%+v, %v), want nil ErrReaderRequired", got, err)
	}

	injected := errors.New("injected reader failure")
	service, err := New(&fakeReader{snapshotErr: injected, messagesErr: injected})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, err := service.Snapshot(
		context.Background(),
		SnapshotQuery{ProjectID: "project-1"},
	); got != nil || !errors.Is(err, injected) {
		t.Fatalf("Snapshot reader error = (%+v, %v), want nil injected error", got, err)
	}
	if got, err := service.Messages(
		context.Background(),
		MessageQuery{ProjectID: "project-1"},
	); got != nil || !errors.Is(err, injected) {
		t.Fatalf("Messages reader error = (%+v, %v), want nil injected error", got, err)
	}

	for name, fixture := range map[string]*store.OperatorSnapshot{
		"nil snapshot": nil,
		"project mismatch": func() *store.OperatorSnapshot {
			raw := operatorSnapshotFixture()
			raw.Project.ID = "project-2"
			return raw
		}(),
		"worker count mismatch": func() *store.OperatorSnapshot {
			raw := operatorSnapshotFixture()
			raw.ActiveWorkerCount = 0
			return raw
		}(),
		"unknown task status": func() *store.OperatorSnapshot {
			raw := operatorSnapshotFixture()
			raw.Tasks.Items[0].Status = store.TaskStatus("future")
			return raw
		}(),
		"plan cursor missing": func() *store.OperatorSnapshot {
			raw := operatorSnapshotFixture()
			raw.Plans.NextAfterID = ""
			return raw
		}(),
		"task cursor incomplete": func() *store.OperatorSnapshot {
			raw := operatorSnapshotFixture()
			raw.Tasks.NextAfter.TaskID = ""
			return raw
		}(),
		"event cursor missing": func() *store.OperatorSnapshot {
			raw := operatorSnapshotFixture()
			raw.RecentEvents.NextBeforeID = 0
			return raw
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			service, err := New(&fakeReader{snapshot: fixture})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			got, err := service.Snapshot(
				context.Background(),
				SnapshotQuery{ProjectID: "project-1"},
			)
			if got != nil || err == nil {
				t.Fatalf("Snapshot = (%+v, %v), want nil projection error", got, err)
			}
		})
	}

	for name, page := range map[string]*store.OperatorMessagePage{
		"nil messages": nil,
		"unknown role": {
			Items: []store.OperatorMessageMetadata{{
				ID: 1, PlanID: "plan-1", TaskID: "task-1", Role: "ROLE_FUTURE",
			}},
		},
		"empty identity": {
			Items: []store.OperatorMessageMetadata{{
				ID: 1, PlanID: "plan-1", Role: string(sdka2a.MessageRoleAgent),
			}},
		},
		"missing page cursor": {
			HasMore: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			service, err := New(&fakeReader{messages: page})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			got, err := service.Messages(
				context.Background(),
				MessageQuery{ProjectID: "project-1"},
			)
			if got != nil || err == nil {
				t.Fatalf("Messages = (%+v, %v), want nil projection error", got, err)
			}
		})
	}
}

func TestServiceDefendsProjectionBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*store.OperatorSnapshot)
	}{
		{
			name: "plans",
			mutate: func(raw *store.OperatorSnapshot) {
				raw.Plans.Items = make([]store.OperatorPlan, store.MaxOperatorPageLimit+1)
			},
		},
		{
			name: "tasks",
			mutate: func(raw *store.OperatorSnapshot) {
				raw.Tasks.Items = make([]store.OperatorTask, store.MaxOperatorPageLimit+1)
			},
		},
		{
			name: "events",
			mutate: func(raw *store.OperatorSnapshot) {
				raw.RecentEvents.Items = make(
					[]store.OperatorEvent,
					store.MaxOperatorEventLimit+1,
				)
			},
		},
		{
			name: "workers",
			mutate: func(raw *store.OperatorSnapshot) {
				raw.Workers = make(
					[]store.OperatorWorker,
					store.MaxOperatorActiveWorkers+1,
				)
				raw.ActiveWorkerCount = len(raw.Workers)
			},
		},
		{
			name: "claims",
			mutate: func(raw *store.OperatorSnapshot) {
				raw.Workers[0].Claims = make(
					[]store.OperatorWorkerClaim,
					store.MaxOperatorActiveClaims+1,
				)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := operatorSnapshotFixture()
			test.mutate(raw)
			service, err := New(&fakeReader{snapshot: raw})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			got, err := service.Snapshot(
				context.Background(),
				SnapshotQuery{ProjectID: "project-1"},
			)
			if got != nil || !errors.Is(err, ErrProjectionLimitBreach) {
				t.Fatalf("Snapshot = (%+v, %v), want nil ErrProjectionLimitBreach", got, err)
			}
		})
	}

	tooManyMessages := &store.OperatorMessagePage{
		Items: make(
			[]store.OperatorMessageMetadata,
			store.MaxOperatorMessageLimit+1,
		),
	}
	service, err := New(&fakeReader{messages: tooManyMessages})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := service.Messages(
		context.Background(),
		MessageQuery{ProjectID: "project-1"},
	)
	if got != nil || !errors.Is(err, ErrProjectionLimitBreach) {
		t.Fatalf("Messages = (%+v, %v), want nil ErrProjectionLimitBreach", got, err)
	}
}

// ListOperatorTaskDescriptions backs the opt-in per-plan description read.
func (f *fakeReader) ListOperatorTaskDescriptions(
	_ context.Context,
	_, _ string,
	_ []string,
) (map[string]string, error) {
	if f.detailErr != nil {
		return nil, f.detailErr
	}
	return f.descriptions, nil
}
