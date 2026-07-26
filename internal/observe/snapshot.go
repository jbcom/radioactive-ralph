// Package observe projects Ralph's durable store into versioned,
// transport-neutral, read-only operator responses.
//
// This package is not an A2A server. It uses the official A2A Task vocabulary
// for task lifecycle projection, but it deliberately exposes no AgentCard,
// URL, transport binding, or network capability. A real authenticated
// transport must exist and be verified before any of those claims are valid.
package observe

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

const (
	// SchemaVersion is the stable JSON schema version for both snapshot and
	// message-metadata responses. It evolves independently of IPC framing.
	SchemaVersion = 1

	// MetadataKey namespaces Ralph-specific fields inside an official A2A
	// Task's extension metadata.
	MetadataKey = "https://jonbogaty.com/radioactive-ralph/a2a/v1"
)

// Projection sentinels let IPC and CLI adapters preserve fail-closed behavior
// without scraping error strings.
var (
	ErrReaderRequired        = errors.New("observe: reader required")
	ErrInvalidReadModel      = errors.New("observe: invalid store read model")
	ErrUnknownTaskStatus     = errors.New("observe: unknown Ralph task status")
	ErrUnknownMessageRole    = errors.New("observe: unknown A2A message role")
	ErrProjectionLimitBreach = errors.New("observe: store response exceeds projection bounds")
)

// Reader is the complete durable query boundary needed by the observation
// service. *store.Store implements it. Keeping this interface read-only makes
// it impossible for an observation transport to mint task completion.
type Reader interface {
	ReadOperatorSnapshot(
		context.Context,
		store.OperatorSnapshotQuery,
	) (*store.OperatorSnapshot, error)
	ListOperatorMessages(
		context.Context,
		store.OperatorMessageQuery,
	) (*store.OperatorMessagePage, error)
}

var _ Reader = (*store.Store)(nil)

// Service projects one safe Reader into versioned transport-neutral DTOs.
// The zero value fails closed with ErrReaderRequired.
type Service struct {
	reader Reader
}

// New constructs a read-only observation service.
func New(reader Reader) (*Service, error) {
	if readerIsNil(reader) {
		return nil, ErrReaderRequired
	}
	return &Service{reader: reader}, nil
}

// SnapshotQuery selects independently bounded pages from one project snapshot.
// Zero limits use the store's documented defaults. Returned cursors can be
// passed directly into the next request.
type SnapshotQuery struct {
	ProjectID     string     `json:"project_id"`
	PlanLimit     int        `json:"plan_limit,omitempty"`
	PlanAfterID   string     `json:"plan_after_id,omitempty"`
	TaskLimit     int        `json:"task_limit,omitempty"`
	TaskAfter     TaskCursor `json:"task_after,omitempty"`
	EventLimit    int        `json:"event_limit,omitempty"`
	EventBeforeID int64      `json:"event_before_id,omitempty"`
}

// TaskCursor is the stable (plan_id, task_id) keyset cursor for task pages.
type TaskCursor struct {
	PlanID string `json:"plan_id"`
	TaskID string `json:"task_id"`
}

// Snapshot is one internally consistent, content-safe operator view. Every
// field derives from one Store.ReadOperatorSnapshot read transaction.
type Snapshot struct {
	SchemaVersion int       `json:"schema_version"`
	CapturedAt    time.Time `json:"captured_at"`
	Project       Project   `json:"project"`
	Summary       Summary   `json:"summary"`
	Plans         PlanPage  `json:"plans"`
	Tasks         TaskPage  `json:"tasks"`
	Workers       []Worker  `json:"workers"`
	RecentEvents  EventPage `json:"recent_events"`
}

// Project is safe project metadata. Repository paths, remotes, and
// fingerprints never enter this projection.
type Project struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"display_name"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
}

// Summary exposes complete automation counts, independent of the current
// plan/task pages. ZeroActiveWorkers is trustworthy only because Snapshot
// returns nil on every read or projection error.
type Summary struct {
	PlanTotal         int           `json:"plan_total"`
	TaskTotal         int           `json:"task_total"`
	ActiveWorkerCount int           `json:"active_worker_count"`
	ZeroActiveWorkers bool          `json:"zero_active_workers"`
	PlanStatusCounts  []StatusCount `json:"plan_status_counts"`
	TaskStatusCounts  []StatusCount `json:"task_status_counts"`
}

// StatusCount is one deterministic status/count pair.
type StatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// Plan is the safe plan projection. Source markdown and tags are absent.
type Plan struct {
	ID        string           `json:"id"`
	Slug      string           `json:"slug"`
	Title     string           `json:"title"`
	Status    store.PlanStatus `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// PlanPage is one deterministic plan page.
type PlanPage struct {
	Items       []Plan `json:"items"`
	HasMore     bool   `json:"has_more"`
	NextAfterID string `json:"next_after_id,omitempty"`
}

// Task is Ralph's safe DAG state plus its official A2A Task lifecycle
// projection. Description, acceptance commands, raw messages, and artifacts
// are intentionally absent.
type Task struct {
	PlanID            string           `json:"plan_id"`
	ID                string           `json:"id"`
	CanonicalID       string           `json:"canonical_id"`
	Status            store.TaskStatus `json:"status"`
	ParallelGroup     *int64           `json:"parallel_group,omitempty"`
	SequenceOrdinal   *int64           `json:"sequence_ordinal,omitempty"`
	RetryCount        int              `json:"retry_count"`
	ReclaimCount      int              `json:"reclaim_count"`
	ParentTaskID      string           `json:"parent_task_id,omitempty"`
	ClaimedByWorkerID string           `json:"claimed_by_worker_id,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	A2ATask           *sdka2a.Task     `json:"a2a_task"`
}

// TaskPage is one deterministic composite-key task page.
type TaskPage struct {
	Items     []Task      `json:"items"`
	HasMore   bool        `json:"has_more"`
	NextAfter *TaskCursor `json:"next_after,omitempty"`
}

// Worker is one active Ralph worker and all of its project task claims.
// Ralph worker IDs are stable control identifiers; process and provider
// session IDs are never projected.
type Worker struct {
	ID            string        `json:"id"`
	Provider      string        `json:"provider"`
	Model         string        `json:"model,omitempty"`
	NativeFanout  bool          `json:"native_fanout"`
	Status        string        `json:"status"`
	StartedAt     time.Time     `json:"started_at"`
	LastHeartbeat time.Time     `json:"last_heartbeat"`
	Claims        []WorkerClaim `json:"claims"`
}

// WorkerClaim is one project task held by a worker.
type WorkerClaim struct {
	PlanID string           `json:"plan_id"`
	TaskID string           `json:"task_id"`
	Status store.TaskStatus `json:"status"`
}

// FailureCategory is a fixed, non-secret operator taxonomy derived only from
// durable event kinds. Raw payload text is never inspected by this package.
type FailureCategory string

// Safe failure categories. These values are a closed schema surface.
const (
	FailureTaskAttempt  FailureCategory = "task_attempt"
	FailureTaskTerminal FailureCategory = "task_terminal"
	FailureVerification FailureCategory = "verification"
	FailureDispatch     FailureCategory = "dispatch"
	FailureAdmission    FailureCategory = "admission"
)

// FailureSummary explains a recognized failure event using only static text.
// It never includes raw provider output, prompts, argv, stderr, or event
// payload fields.
type FailureSummary struct {
	Category  FailureCategory `json:"category"`
	Summary   string          `json:"summary"`
	Retryable bool            `json:"retryable"`
}

// Event is bounded safe event metadata. Failure is present only for a closed
// set of recognized failure kinds and contains a static summary.
type Event struct {
	ID         int64           `json:"id"`
	PlanID     string          `json:"plan_id,omitempty"`
	TaskID     string          `json:"task_id,omitempty"`
	Kind       string          `json:"kind"`
	Stream     string          `json:"stream,omitempty"`
	OccurredAt time.Time       `json:"occurred_at"`
	Failure    *FailureSummary `json:"failure,omitempty"`
}

// EventPage is one newest-first event metadata page.
type EventPage struct {
	Items        []Event `json:"items"`
	HasMore      bool    `json:"has_more"`
	NextBeforeID int64   `json:"next_before_id,omitempty"`
}

// MessageQuery selects bounded, oldest-first, content-free A2A message
// metadata. TaskID requires PlanID; the store validates every scope and cursor.
type MessageQuery struct {
	ProjectID string `json:"project_id"`
	PlanID    string `json:"plan_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	AfterID   int64  `json:"after_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// MessageMetadata is the safe A2A envelope index. It deliberately is not an
// a2a.Message: message Parts are content and remain behind the store boundary.
type MessageMetadata struct {
	ID              int64              `json:"id"`
	WorkerID        string             `json:"worker_id,omitempty"`
	PlanID          string             `json:"plan_id"`
	TaskID          string             `json:"task_id"`
	CanonicalTaskID string             `json:"canonical_task_id"`
	ContextID       string             `json:"context_id"`
	Role            sdka2a.MessageRole `json:"role"`
	OccurredAt      time.Time          `json:"occurred_at"`
}

// MessagePage is one versioned chronological metadata page.
type MessagePage struct {
	SchemaVersion int               `json:"schema_version"`
	Items         []MessageMetadata `json:"items"`
	HasMore       bool              `json:"has_more"`
	NextAfterID   int64             `json:"next_after_id,omitempty"`
}

// Snapshot reads and projects one project snapshot. Errors always return nil,
// so no caller can mistake zero counts or empty pages for a valid result.
func (s *Service) Snapshot(
	ctx context.Context,
	q SnapshotQuery,
) (*Snapshot, error) {
	if s == nil || readerIsNil(s.reader) {
		return nil, ErrReaderRequired
	}
	raw, err := s.reader.ReadOperatorSnapshot(ctx, store.OperatorSnapshotQuery{
		ProjectID:     q.ProjectID,
		PlanLimit:     q.PlanLimit,
		PlanAfterID:   q.PlanAfterID,
		TaskLimit:     q.TaskLimit,
		TaskAfter:     store.OperatorTaskCursor(q.TaskAfter),
		EventLimit:    q.EventLimit,
		EventBeforeID: q.EventBeforeID,
	})
	if err != nil {
		return nil, fmt.Errorf("observe: read snapshot: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("%w: nil snapshot", ErrInvalidReadModel)
	}
	if err := validateSnapshotReadModel(raw, q.ProjectID); err != nil {
		return nil, err
	}

	out := &Snapshot{
		SchemaVersion: SchemaVersion,
		CapturedAt:    raw.CapturedAt,
		Project:       projectFromStore(raw.Project),
		Summary: Summary{
			ActiveWorkerCount: raw.ActiveWorkerCount,
			ZeroActiveWorkers: raw.ActiveWorkerCount == 0,
			PlanStatusCounts:  statusCountsFromStore(raw.PlanCounts),
			TaskStatusCounts:  statusCountsFromStore(raw.TaskCounts),
		},
		Plans:        plansFromStore(raw.Plans),
		Workers:      workersFromStore(raw.Workers),
		RecentEvents: eventsFromStore(raw.RecentEvents),
	}
	out.Summary.PlanTotal = totalCount(out.Summary.PlanStatusCounts)
	out.Summary.TaskTotal = totalCount(out.Summary.TaskStatusCounts)

	tasks, err := tasksFromStore(raw.Tasks)
	if err != nil {
		return nil, err
	}
	out.Tasks = tasks
	return out, nil
}

// Messages reads and projects one bounded chronological A2A message metadata
// page. Message content is never requested from the Reader.
func (s *Service) Messages(
	ctx context.Context,
	q MessageQuery,
) (*MessagePage, error) {
	if s == nil || readerIsNil(s.reader) {
		return nil, ErrReaderRequired
	}
	raw, err := s.reader.ListOperatorMessages(ctx, store.OperatorMessageQuery{
		ProjectID: q.ProjectID,
		PlanID:    q.PlanID,
		TaskID:    q.TaskID,
		AfterID:   q.AfterID,
		Limit:     q.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("observe: read messages: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("%w: nil message page", ErrInvalidReadModel)
	}
	if len(raw.Items) > store.MaxOperatorMessageLimit {
		return nil, fmt.Errorf(
			"%w: %d messages",
			ErrProjectionLimitBreach,
			len(raw.Items),
		)
	}
	if raw.HasMore && raw.NextAfterID <= 0 {
		return nil, fmt.Errorf(
			"%w: message page has_more without cursor",
			ErrInvalidReadModel,
		)
	}
	if !raw.HasMore && raw.NextAfterID != 0 {
		return nil, fmt.Errorf(
			"%w: terminal message page has cursor",
			ErrInvalidReadModel,
		)
	}

	out := &MessagePage{
		SchemaVersion: SchemaVersion,
		Items:         make([]MessageMetadata, 0, len(raw.Items)),
		HasMore:       raw.HasMore,
		NextAfterID:   raw.NextAfterID,
	}
	for _, item := range raw.Items {
		role, err := messageRole(item.Role)
		if err != nil {
			return nil, err
		}
		if item.PlanID == "" || item.TaskID == "" {
			return nil, fmt.Errorf(
				"%w: message %d has empty task identity",
				ErrInvalidReadModel,
				item.ID,
			)
		}
		out.Items = append(out.Items, MessageMetadata{
			ID:              item.ID,
			WorkerID:        item.WorkerID,
			PlanID:          item.PlanID,
			TaskID:          item.TaskID,
			CanonicalTaskID: canonicalTaskID(item.PlanID, item.TaskID),
			ContextID:       item.PlanID,
			Role:            role,
			OccurredAt:      item.OccurredAt,
		})
	}
	return out, nil
}

func validateSnapshotReadModel(raw *store.OperatorSnapshot, projectID string) error {
	if raw.Project.ID == "" || raw.Project.ID != projectID {
		return fmt.Errorf(
			"%w: project mismatch (got %q, want %q)",
			ErrInvalidReadModel,
			raw.Project.ID,
			projectID,
		)
	}
	if raw.ActiveWorkerCount != len(raw.Workers) {
		return fmt.Errorf(
			"%w: active worker count %d differs from rows %d",
			ErrInvalidReadModel,
			raw.ActiveWorkerCount,
			len(raw.Workers),
		)
	}
	if len(raw.Plans.Items) > store.MaxOperatorPageLimit {
		return fmt.Errorf(
			"%w: %d plans",
			ErrProjectionLimitBreach,
			len(raw.Plans.Items),
		)
	}
	if len(raw.Tasks.Items) > store.MaxOperatorPageLimit {
		return fmt.Errorf(
			"%w: %d tasks",
			ErrProjectionLimitBreach,
			len(raw.Tasks.Items),
		)
	}
	if len(raw.RecentEvents.Items) > store.MaxOperatorEventLimit {
		return fmt.Errorf(
			"%w: %d events",
			ErrProjectionLimitBreach,
			len(raw.RecentEvents.Items),
		)
	}
	if len(raw.Workers) > store.MaxOperatorActiveWorkers {
		return fmt.Errorf(
			"%w: %d active workers",
			ErrProjectionLimitBreach,
			len(raw.Workers),
		)
	}
	if raw.Plans.HasMore && raw.Plans.NextAfterID == "" {
		return fmt.Errorf(
			"%w: plan page has_more without cursor",
			ErrInvalidReadModel,
		)
	}
	if !raw.Plans.HasMore && raw.Plans.NextAfterID != "" {
		return fmt.Errorf(
			"%w: terminal plan page has cursor",
			ErrInvalidReadModel,
		)
	}
	if raw.Tasks.HasMore &&
		(raw.Tasks.NextAfter == nil ||
			raw.Tasks.NextAfter.PlanID == "" ||
			raw.Tasks.NextAfter.TaskID == "") {
		return fmt.Errorf(
			"%w: task page has_more without complete cursor",
			ErrInvalidReadModel,
		)
	}
	if !raw.Tasks.HasMore && raw.Tasks.NextAfter != nil {
		return fmt.Errorf(
			"%w: terminal task page has cursor",
			ErrInvalidReadModel,
		)
	}
	if raw.RecentEvents.HasMore && raw.RecentEvents.NextBeforeID <= 0 {
		return fmt.Errorf(
			"%w: event page has_more without cursor",
			ErrInvalidReadModel,
		)
	}
	if !raw.RecentEvents.HasMore && raw.RecentEvents.NextBeforeID != 0 {
		return fmt.Errorf(
			"%w: terminal event page has cursor",
			ErrInvalidReadModel,
		)
	}
	claims := 0
	for _, worker := range raw.Workers {
		claims += len(worker.Claims)
	}
	if claims > store.MaxOperatorActiveClaims {
		return fmt.Errorf(
			"%w: %d worker claims",
			ErrProjectionLimitBreach,
			claims,
		)
	}
	return nil
}

func readerIsNil(reader Reader) bool {
	if reader == nil {
		return true
	}
	value := reflect.ValueOf(reader)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func projectFromStore(project store.OperatorProject) Project {
	return Project{
		ID:          project.ID,
		DisplayName: project.DisplayName,
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
		LastSeenAt:  project.LastSeenAt,
	}
}

func statusCountsFromStore(counts []store.OperatorStatusCount) []StatusCount {
	out := make([]StatusCount, 0, len(counts))
	for _, count := range counts {
		out = append(out, StatusCount{Status: count.Status, Count: count.Count})
	}
	return out
}

func totalCount(counts []StatusCount) int {
	total := 0
	for _, count := range counts {
		total += count.Count
	}
	return total
}

func plansFromStore(page store.OperatorPlanPage) PlanPage {
	out := PlanPage{
		Items:       make([]Plan, 0, len(page.Items)),
		HasMore:     page.HasMore,
		NextAfterID: page.NextAfterID,
	}
	for _, item := range page.Items {
		out.Items = append(out.Items, Plan{
			ID:        item.ID,
			Slug:      item.Slug,
			Title:     item.Title,
			Status:    item.Status,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		})
	}
	return out
}

func tasksFromStore(page store.OperatorTaskPage) (TaskPage, error) {
	out := TaskPage{
		Items:   make([]Task, 0, len(page.Items)),
		HasMore: page.HasMore,
	}
	if page.NextAfter != nil {
		out.NextAfter = &TaskCursor{
			PlanID: page.NextAfter.PlanID,
			TaskID: page.NextAfter.TaskID,
		}
	}
	for _, item := range page.Items {
		task, err := taskFromStore(item)
		if err != nil {
			return TaskPage{}, err
		}
		out.Items = append(out.Items, task)
	}
	return out, nil
}

func taskFromStore(item store.OperatorTask) (Task, error) {
	if item.PlanID == "" || item.ID == "" {
		return Task{}, fmt.Errorf(
			"%w: task has empty identity",
			ErrInvalidReadModel,
		)
	}
	state, err := StateForTask(item.Status)
	if err != nil {
		return Task{}, err
	}
	canonicalID := canonicalTaskID(item.PlanID, item.ID)
	statusTime := item.UpdatedAt
	protocolTask := &sdka2a.Task{
		ID:        sdka2a.TaskID(canonicalID),
		ContextID: item.PlanID,
		Status: sdka2a.TaskStatus{
			State:     state,
			Timestamp: &statusTime,
		},
		Metadata: map[string]any{
			MetadataKey: map[string]any{
				"plan_id":       item.PlanID,
				"task_id":       item.ID,
				"ralph_status":  string(item.Status),
				"retry_count":   item.RetryCount,
				"reclaim_count": item.ReclaimCount,
			},
		},
	}
	return Task{
		PlanID:            item.PlanID,
		ID:                item.ID,
		CanonicalID:       canonicalID,
		Status:            item.Status,
		ParallelGroup:     item.ParallelGroup,
		SequenceOrdinal:   item.SequenceOrdinal,
		RetryCount:        item.RetryCount,
		ReclaimCount:      item.ReclaimCount,
		ParentTaskID:      item.ParentTaskID,
		ClaimedByWorkerID: item.ClaimedByWorkerID,
		CreatedAt:         item.CreatedAt,
		UpdatedAt:         item.UpdatedAt,
		A2ATask:           protocolTask,
	}, nil
}

func workersFromStore(items []store.OperatorWorker) []Worker {
	out := make([]Worker, 0, len(items))
	for _, item := range items {
		claims := make([]WorkerClaim, 0, len(item.Claims))
		for _, claim := range item.Claims {
			claims = append(claims, WorkerClaim{
				PlanID: claim.PlanID,
				TaskID: claim.TaskID,
				Status: claim.Status,
			})
		}
		out = append(out, Worker{
			ID:            item.ID,
			Provider:      item.Provider,
			Model:         item.Model,
			NativeFanout:  item.NativeFanout,
			Status:        item.Status,
			StartedAt:     item.StartedAt,
			LastHeartbeat: item.LastHeartbeat,
			Claims:        claims,
		})
	}
	return out
}

func eventsFromStore(page store.OperatorEventPage) EventPage {
	out := EventPage{
		Items:        make([]Event, 0, len(page.Items)),
		HasMore:      page.HasMore,
		NextBeforeID: page.NextBeforeID,
	}
	for _, item := range page.Items {
		out.Items = append(out.Items, EventFromMetadata(item))
	}
	return out
}

// EventFromMetadata projects one already-scoped safe store event into the
// public event shape. Live Attach uses this same function as snapshot backlog,
// so their privacy and failure-taxonomy contracts cannot drift.
func EventFromMetadata(item store.OperatorEvent) Event {
	return Event{
		ID:         item.ID,
		PlanID:     item.PlanID,
		TaskID:     item.TaskID,
		Kind:       item.Kind,
		Stream:     item.Stream,
		OccurredAt: item.OccurredAt,
		Failure:    failureForEvent(item.Kind),
	}
}

// StateForTask maps durable Ralph status onto the official A2A v2.3.1 task
// lifecycle. Only durable orchestrator-produced terminal statuses map to
// Completed; unknown values fail the entire projection.
func StateForTask(status store.TaskStatus) (sdka2a.TaskState, error) {
	switch status {
	case store.TaskStatusPending, store.TaskStatusReady:
		return sdka2a.TaskStateSubmitted, nil
	case store.TaskStatusRunning:
		return sdka2a.TaskStateWorking, nil
	case store.TaskStatusBlocked, store.TaskStatusReadyPendingApproval:
		return sdka2a.TaskStateInputRequired, nil
	case store.TaskStatusDone, store.TaskStatusDecomposed:
		return sdka2a.TaskStateCompleted, nil
	case store.TaskStatusFailed:
		return sdka2a.TaskStateFailed, nil
	case store.TaskStatusSkipped:
		return sdka2a.TaskStateCanceled, nil
	default:
		return sdka2a.TaskStateUnspecified, fmt.Errorf(
			"%w: %q",
			ErrUnknownTaskStatus,
			status,
		)
	}
}

func messageRole(role string) (sdka2a.MessageRole, error) {
	switch role {
	case string(sdka2a.MessageRoleAgent):
		return sdka2a.MessageRoleAgent, nil
	case string(sdka2a.MessageRoleUser):
		return sdka2a.MessageRoleUser, nil
	default:
		return sdka2a.MessageRoleUnspecified, fmt.Errorf(
			"%w: %q",
			ErrUnknownMessageRole,
			role,
		)
	}
}

func canonicalTaskID(planID, taskID string) string {
	return planID + ":" + taskID
}

func failureForEvent(kind string) *FailureSummary {
	var failure FailureSummary
	switch kind {
	case "task.failed":
		failure = FailureSummary{
			Category:  FailureTaskAttempt,
			Summary:   "task attempt failed and was requeued",
			Retryable: true,
		}
	case "task.failed_terminal":
		failure = FailureSummary{
			Category:  FailureTaskTerminal,
			Summary:   "task retry budget was exhausted",
			Retryable: false,
		}
	case "worker.verification_failed":
		failure = FailureSummary{
			Category:  FailureVerification,
			Summary:   "completion evidence failed verification",
			Retryable: true,
		}
	case "worker.dispatch_error":
		failure = FailureSummary{
			Category:  FailureDispatch,
			Summary:   "worker dispatch failed",
			Retryable: true,
		}
	case "worker.dispatch_panic":
		failure = FailureSummary{
			Category:  FailureDispatch,
			Summary:   "worker dispatch failed unexpectedly",
			Retryable: true,
		}
	case "worker.admission_refused":
		failure = FailureSummary{
			Category:  FailureAdmission,
			Summary:   "worker admission was refused",
			Retryable: false,
		}
	default:
		return nil
	}
	return &failure
}
