package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Operator-query defaults and hard bounds. Snapshot plan/task pages are
// independently paginated; active workers and their claims are returned in
// full up to hard safety ceilings so an automation gate can never mistake a
// truncated worker list for the truth.
const (
	DefaultOperatorPageLimit  = 50
	MaxOperatorPageLimit      = 200
	DefaultOperatorEventLimit = 20
	MaxOperatorEventLimit     = 100
	MaxOperatorActiveWorkers  = 256
	MaxOperatorActiveClaims   = 2048

	maxOperatorStatusKinds = 32
)

// Operator-query sentinels let projection/IPC layers fail closed without
// scraping error strings.
var (
	ErrOperatorInvalidQuery     = errors.New("store: invalid operator query")
	ErrOperatorProjectNotFound  = errors.New("store: operator project not found")
	ErrOperatorPlanNotFound     = errors.New("store: operator plan not found")
	ErrOperatorTaskNotFound     = errors.New("store: operator task not found")
	ErrOperatorInvalidCursor    = errors.New("store: invalid operator cursor")
	ErrOperatorSnapshotTooLarge = errors.New(
		"store: operator snapshot exceeds safety bounds",
	)
)

// OperatorSnapshotQuery selects finite pages from one project's consistent
// operator snapshot. Zero limits use the exported defaults. Cursors are
// keyset cursors from a prior page and are validated against ProjectID before
// any page is read.
type OperatorSnapshotQuery struct {
	ProjectID     string             `json:"project_id"`
	PlanID        string             `json:"plan_id"`
	TaskID        string             `json:"task_id"`
	PlanLimit     int                `json:"plan_limit"`
	PlanAfterID   string             `json:"plan_after_id"`
	TaskLimit     int                `json:"task_limit"`
	TaskAfter     OperatorTaskCursor `json:"task_after"`
	EventLimit    int                `json:"event_limit"`
	EventBeforeID int64              `json:"event_before_id"`
}

// OperatorTaskCursor is the deterministic keyset cursor for project tasks,
// which are ordered by (plan_id, task_id).
type OperatorTaskCursor struct {
	PlanID string `json:"plan_id"`
	TaskID string `json:"task_id"`
}

// OperatorSnapshot is a content-safe, project-scoped read model. It
// deliberately excludes plan markdown, task descriptions and acceptance
// commands, raw event payloads/actors, project identifiers and paths,
// process/session identifiers, and configuration.
type OperatorSnapshot struct {
	CapturedAt        time.Time             `json:"captured_at"`
	Project           OperatorProject       `json:"project"`
	PlanCounts        []OperatorStatusCount `json:"plan_counts"`
	TaskCounts        []OperatorStatusCount `json:"task_counts"`
	Plans             OperatorPlanPage      `json:"plans"`
	Tasks             OperatorTaskPage      `json:"tasks"`
	ActiveWorkerCount int                   `json:"active_worker_count"`
	Workers           []OperatorWorker      `json:"workers"`
	EventCursor       int64                 `json:"event_cursor"`
	RecentEvents      OperatorEventPage     `json:"recent_events"`
}

// OperatorProject is safe project metadata. Identity fingerprints and paths
// never enter the operator DTO.
type OperatorProject struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"display_name"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
}

// OperatorStatusCount is one deterministic status/count pair.
type OperatorStatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// OperatorPlan is a safe plan projection. SourceMarkdown, TagsJSON, and
// session information are intentionally absent.
type OperatorPlan struct {
	ID        string     `json:"id"`
	Slug      string     `json:"slug"`
	Title     string     `json:"title"`
	Status    PlanStatus `json:"status"`
	TaskDone  int        `json:"task_done"`
	TaskTotal int        `json:"task_total"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// OperatorPlanPage is one bounded, ID-ordered plan page.
type OperatorPlanPage struct {
	Items       []OperatorPlan `json:"items"`
	HasMore     bool           `json:"has_more"`
	NextAfterID string         `json:"next_after_id"`
}

// OperatorTask is safe task state. Description and AcceptanceJSON can contain
// source text, commands, or repository paths, so neither is projected.
type OperatorTask struct {
	PlanID            string     `json:"plan_id"`
	ID                string     `json:"id"`
	Status            TaskStatus `json:"status"`
	ParallelGroup     *int64     `json:"parallel_group"`
	SequenceOrdinal   *int64     `json:"sequence_ordinal"`
	RetryCount        int        `json:"retry_count"`
	ReclaimCount      int        `json:"reclaim_count"`
	ParentTaskID      string     `json:"parent_task_id"`
	ClaimedByWorkerID string     `json:"claimed_by_worker_id"`

	// Assigned* is execution provenance: which provider actually ran this task,
	// as recorded by RecordTaskExecution. Empty until the task runs, and
	// deliberately not defaulted -- "never dispatched" must stay distinguishable
	// from "ran on the pool default".
	//
	// Projected per task because it OUTLIVES the worker. claimed_by_worker_id is
	// a live claim: the reaper deletes worker rows once they stop heartbeating,
	// and a finished task releases its claim -- so a done, failed, or reaped task
	// has no worker row left to ask. This lives in the task's own metadata and
	// still answers "what ran it?" afterwards. It also survives reassignment: a
	// retry or reclaim overwrites the assignment, so this names the provider of
	// the CURRENT attempt.
	//
	// Within one native fan-out group these agree by construction (one turn, one
	// binding, recorded onto every task in the group) -- the value here is
	// durability across time, not disagreement within a group.
	AssignedAlias              string `json:"assigned_alias"`
	AssignedProvider           string `json:"assigned_provider"`
	AssignedModel              string `json:"assigned_model"`
	AssignedEffort             string `json:"assigned_effort"`
	AssignedIndependenceDomain string `json:"assigned_independence_domain"`

	// PartitionOrdinal is the opaque identity of the ready-partition this task
	// belongs to: tasks sharing it are the ones native fan-out may delegate to a
	// single provider turn. It exists so an operator can SEE that grouping --
	// otherwise five simultaneously-running tasks look alike whether they are
	// one fan-out turn or five independent dispatches.
	//
	// Opaque on purpose. A partition's real identity is (group path, declared
	// binding key), and the binding key re-encodes the author's own binding
	// fields, so exposing it would carry plan-authored text across a boundary
	// that withholds descriptions and acceptance commands. The ordinal answers
	// "same partition or not?" without answering "pinned to what?".
	PartitionOrdinal string `json:"partition_ordinal"`

	// BlockedByTaskID names a dependency that will NEVER be satisfied, empty
	// otherwise. It answers the one question a stalled plan raises that status
	// alone cannot: is this waiting, or is it dead?
	//
	// Only TERMINAL blockers are named, and that word is load-bearing rather
	// than descriptive: both readiness walks satisfy a dependency solely on
	// done/skipped/decomposed, MarkFailedWithPayload retries by setting pending
	// and lands on failed once retries are exhausted, and no transition leaves
	// failed. So a dependent behind a failed task can never run, while one
	// behind a merely-incomplete task clears itself -- naming the second would
	// be noise on every healthy plan mid-flight.
	BlockedByTaskID string `json:"blocked_by_task_id"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// OperatorTaskPage is one bounded, (plan_id, task_id)-ordered task page.
type OperatorTaskPage struct {
	Items     []OperatorTask      `json:"items"`
	HasMore   bool                `json:"has_more"`
	NextAfter *OperatorTaskCursor `json:"next_after"`
}

// OperatorWorker is one active Ralph-managed worker. Ralph worker IDs are
// operator controls (for example worker kill), not provider-session IDs.
// Claims contains every task claimed by the worker in this project, including
// native fan-out claims beyond workers.current_task_id.
type OperatorWorker struct {
	ID            string                `json:"id"`
	Provider      string                `json:"provider"`
	Model         string                `json:"model"`
	NativeFanout  bool                  `json:"native_fanout"`
	Status        string                `json:"status"`
	StartedAt     time.Time             `json:"started_at"`
	LastHeartbeat time.Time             `json:"last_heartbeat"`
	Claims        []OperatorWorkerClaim `json:"claims"`
}

// OperatorWorkerClaim is one project task held by an active worker.
type OperatorWorkerClaim struct {
	PlanID string     `json:"plan_id"`
	TaskID string     `json:"task_id"`
	Status TaskStatus `json:"status"`
}

// OperatorEvent is safe event metadata. Raw payload, actor/provider output,
// and any IDs carried inside the payload are never selected.
type OperatorEvent struct {
	ID         int64     `json:"id"`
	PlanID     string    `json:"plan_id"`
	TaskID     string    `json:"task_id"`
	Kind       string    `json:"kind"`
	Stream     string    `json:"stream"`
	OccurredAt time.Time `json:"occurred_at"`
	// FailureCategory is the provider failure code from the event payload, or
	// "" when the event carries none.
	//
	// This is the ONLY payload field selected here, and it is safe precisely
	// because it is a CLOSED SET of fixed constants (see provider.Failure) —
	// never provider prose. The rest of payload_json stays off this DTO: it can
	// contain arbitrary text from an external process, which is what the
	// content-safety boundary exists to keep away from operator surfaces.
	FailureCategory string `json:"failure_category,omitempty"`
}

// OperatorEventPage is one bounded newest-first event page. NextBeforeID is a
// keyset cursor that continues toward older events.
type OperatorEventPage struct {
	Items        []OperatorEvent `json:"items"`
	HasMore      bool            `json:"has_more"`
	NextBeforeID int64           `json:"next_before_id"`
}

// ReadOperatorSnapshot reads all snapshot components from one SQLite read
// transaction. A non-nil result is therefore internally consistent: plans,
// tasks, complete active-worker claims, counts, and recent safe event metadata
// all describe the same database snapshot.
//
// The pointer return is intentional. Any query/scan/commit failure returns a
// nil snapshot, so a caller cannot accidentally interpret a partial result's
// zero ActiveWorkerCount as a valid zero-worker automation gate.
func (s *Store) ReadOperatorSnapshot(
	ctx context.Context,
	q OperatorSnapshotQuery,
) (*OperatorSnapshot, error) {
	return s.readOperatorSnapshot(ctx, q, nil)
}

type operatorSnapshotHooks struct {
	afterProjectRead func() error
}

func (s *Store) readOperatorSnapshot(
	ctx context.Context,
	q OperatorSnapshotQuery,
	hooks *operatorSnapshotHooks,
) (*OperatorSnapshot, error) {
	planLimit, taskLimit, eventLimit, err := validateOperatorSnapshotQuery(q)
	if err != nil {
		return nil, err
	}

	// modernc.org/sqlite deliberately starts ReadOnly transactions with plain
	// BEGIN even when the DSN uses _txlock=immediate. That gives us SQLite's
	// consistent deferred read snapshot without taking the writer reservation
	// used by Ralph's mutation transactions.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("store: begin operator snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	project, err := readOperatorProject(ctx, tx, q.ProjectID)
	if err != nil {
		return nil, err
	}
	if hooks != nil && hooks.afterProjectRead != nil {
		if err := hooks.afterProjectRead(); err != nil {
			return nil, fmt.Errorf("store: operator snapshot test hook: %w", err)
		}
	}
	if err := validateOperatorSnapshotScopeAndCursors(ctx, tx, q); err != nil {
		return nil, err
	}

	planCounts, err := readOperatorPlanCounts(ctx, tx, q.ProjectID)
	if err != nil {
		return nil, err
	}
	taskCounts, err := readOperatorTaskCounts(ctx, tx, q.ProjectID)
	if err != nil {
		return nil, err
	}
	plans, err := readOperatorPlans(
		ctx,
		tx,
		q.ProjectID,
		q.PlanID,
		q.PlanAfterID,
		planLimit,
	)
	if err != nil {
		return nil, err
	}
	tasks, err := readOperatorTasks(
		ctx,
		tx,
		q.ProjectID,
		q.PlanID,
		q.TaskID,
		q.TaskAfter,
		taskLimit,
	)
	if err != nil {
		return nil, err
	}
	workers, err := readOperatorWorkers(ctx, tx, q.ProjectID)
	if err != nil {
		return nil, err
	}
	eventCursor, err := readOperatorEventCursor(ctx, tx, q.ProjectID)
	if err != nil {
		return nil, err
	}
	events, err := readOperatorEvents(
		ctx,
		tx,
		q.ProjectID,
		q.PlanID,
		q.TaskID,
		q.EventBeforeID,
		eventLimit,
	)
	if err != nil {
		return nil, err
	}

	snapshot := &OperatorSnapshot{
		CapturedAt:        s.clock.Now().UTC(),
		Project:           project,
		PlanCounts:        planCounts,
		TaskCounts:        taskCounts,
		Plans:             plans,
		Tasks:             tasks,
		ActiveWorkerCount: len(workers),
		Workers:           workers,
		EventCursor:       eventCursor,
		RecentEvents:      events,
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit operator snapshot: %w", err)
	}
	return snapshot, nil
}

func validateOperatorSnapshotQuery(q OperatorSnapshotQuery) (planLimit, taskLimit, eventLimit int, err error) {
	if q.ProjectID == "" {
		return 0, 0, 0, fmt.Errorf("%w: project_id is required", ErrOperatorInvalidQuery)
	}
	if q.EventBeforeID < 0 {
		return 0, 0, 0, fmt.Errorf("%w: event_before_id must be non-negative", ErrOperatorInvalidQuery)
	}
	if q.TaskID != "" && q.PlanID == "" {
		return 0, 0, 0, fmt.Errorf(
			"%w: task_id requires plan_id",
			ErrOperatorInvalidQuery,
		)
	}
	if q.PlanID != "" && q.PlanAfterID != "" {
		return 0, 0, 0, fmt.Errorf(
			"%w: plan_after_id cannot be combined with plan_id",
			ErrOperatorInvalidQuery,
		)
	}
	if (q.TaskAfter.PlanID == "") != (q.TaskAfter.TaskID == "") {
		return 0, 0, 0, fmt.Errorf(
			"%w: task_after requires both plan_id and task_id",
			ErrOperatorInvalidQuery,
		)
	}
	if q.TaskID != "" && q.TaskAfter.PlanID != "" {
		return 0, 0, 0, fmt.Errorf(
			"%w: task_after cannot be combined with task_id",
			ErrOperatorInvalidQuery,
		)
	}
	if q.PlanID != "" &&
		q.TaskAfter.PlanID != "" &&
		q.TaskAfter.PlanID != q.PlanID {
		return 0, 0, 0, fmt.Errorf(
			"%w: task cursor is outside plan scope",
			ErrOperatorInvalidCursor,
		)
	}
	planLimit, err = normalizeOperatorLimit(
		"plan_limit",
		q.PlanLimit,
		DefaultOperatorPageLimit,
		MaxOperatorPageLimit,
	)
	if err != nil {
		return 0, 0, 0, err
	}
	taskLimit, err = normalizeOperatorLimit(
		"task_limit",
		q.TaskLimit,
		DefaultOperatorPageLimit,
		MaxOperatorPageLimit,
	)
	if err != nil {
		return 0, 0, 0, err
	}
	eventLimit, err = normalizeOperatorLimit(
		"event_limit",
		q.EventLimit,
		DefaultOperatorEventLimit,
		MaxOperatorEventLimit,
	)
	if err != nil {
		return 0, 0, 0, err
	}
	return planLimit, taskLimit, eventLimit, nil
}

func normalizeOperatorLimit(name string, value, defaultValue, maxValue int) (int, error) {
	if value == 0 {
		return defaultValue, nil
	}
	if value < 0 || value > maxValue {
		return 0, fmt.Errorf(
			"%w: %s must be between 1 and %d",
			ErrOperatorInvalidQuery,
			name,
			maxValue,
		)
	}
	return value, nil
}

func readOperatorProject(ctx context.Context, tx *sql.Tx, projectID string) (OperatorProject, error) {
	var project OperatorProject
	var createdRaw, updatedRaw string
	var lastSeenRaw sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT id, COALESCE(display_name, ''), created_at, updated_at, last_seen_at
		FROM projects
		WHERE id = ?
	`, projectID).Scan(
		&project.ID,
		&project.DisplayName,
		&createdRaw,
		&updatedRaw,
		&lastSeenRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return OperatorProject{}, fmt.Errorf("%w: %s", ErrOperatorProjectNotFound, projectID)
	}
	if err != nil {
		return OperatorProject{}, fmt.Errorf("store: read operator project: %w", err)
	}
	project.CreatedAt, err = parseOperatorTimestamp("project.created_at", createdRaw)
	if err != nil {
		return OperatorProject{}, err
	}
	project.UpdatedAt, err = parseOperatorTimestamp("project.updated_at", updatedRaw)
	if err != nil {
		return OperatorProject{}, err
	}
	if lastSeenRaw.Valid {
		lastSeen, parseErr := parseOperatorTimestamp("project.last_seen_at", lastSeenRaw.String)
		if parseErr != nil {
			return OperatorProject{}, parseErr
		}
		project.LastSeenAt = &lastSeen
	}
	return project, nil
}

func validateOperatorSnapshotScopeAndCursors(
	ctx context.Context,
	tx *sql.Tx,
	q OperatorSnapshotQuery,
) error {
	if q.PlanID != "" {
		var exists int
		err := tx.QueryRowContext(ctx, `
			SELECT 1 FROM plans WHERE project_id = ? AND id = ?
		`, q.ProjectID, q.PlanID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrOperatorPlanNotFound, q.PlanID)
		}
		if err != nil {
			return fmt.Errorf("store: validate operator plan scope: %w", err)
		}
	}
	if q.TaskID != "" {
		var exists int
		err := tx.QueryRowContext(ctx, `
			SELECT 1 FROM tasks WHERE plan_id = ? AND id = ?
		`, q.PlanID, q.TaskID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s/%s", ErrOperatorTaskNotFound, q.PlanID, q.TaskID)
		}
		if err != nil {
			return fmt.Errorf("store: validate operator task scope: %w", err)
		}
	}
	if q.PlanAfterID != "" {
		var exists int
		err := tx.QueryRowContext(ctx, `
			SELECT 1 FROM plans WHERE project_id = ? AND id = ?
		`, q.ProjectID, q.PlanAfterID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: plan cursor", ErrOperatorInvalidCursor)
		}
		if err != nil {
			return fmt.Errorf("store: validate plan cursor: %w", err)
		}
	}
	if q.TaskAfter.PlanID != "" {
		var exists int
		err := tx.QueryRowContext(ctx, `
			SELECT 1
			FROM tasks t
			JOIN plans p ON p.id = t.plan_id
			WHERE p.project_id = ? AND t.plan_id = ? AND t.id = ?
		`, q.ProjectID, q.TaskAfter.PlanID, q.TaskAfter.TaskID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: task cursor", ErrOperatorInvalidCursor)
		}
		if err != nil {
			return fmt.Errorf("store: validate task cursor: %w", err)
		}
	}
	if q.EventBeforeID > 0 {
		var exists int
		err := tx.QueryRowContext(ctx, `
			SELECT 1
			FROM events
			WHERE id = ? AND `+eventProjectScope+`
			  AND (? = '' OR plan_id = ?)
			  AND (? = '' OR task_id = ?)
			`,
			q.EventBeforeID,
			q.ProjectID,
			q.ProjectID,
			q.PlanID,
			q.PlanID,
			q.TaskID,
			q.TaskID,
		).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: event cursor", ErrOperatorInvalidCursor)
		}
		if err != nil {
			return fmt.Errorf("store: validate event cursor: %w", err)
		}
	}
	return nil
}

func readOperatorPlanCounts(
	ctx context.Context,
	tx *sql.Tx,
	projectID string,
) ([]OperatorStatusCount, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT status, COUNT(*)
		FROM plans
		WHERE project_id = ?
		GROUP BY status
		ORDER BY status
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: read operator plan counts: %w", err)
	}
	return scanOperatorStatusCounts(rows, "plan")
}

func readOperatorTaskCounts(
	ctx context.Context,
	tx *sql.Tx,
	projectID string,
) ([]OperatorStatusCount, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT t.status, COUNT(*)
		FROM tasks t
		JOIN plans p ON p.id = t.plan_id
		WHERE p.project_id = ?
		GROUP BY t.status
		ORDER BY t.status
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: read operator task counts: %w", err)
	}
	return scanOperatorStatusCounts(rows, "task")
}

func scanOperatorStatusCounts(rows *sql.Rows, kind string) ([]OperatorStatusCount, error) {
	defer func() { _ = rows.Close() }()

	out := make([]OperatorStatusCount, 0)
	for rows.Next() {
		if len(out) == maxOperatorStatusKinds {
			return nil, fmt.Errorf(
				"%w: more than %d %s status kinds",
				ErrOperatorSnapshotTooLarge,
				maxOperatorStatusKinds,
				kind,
			)
		}
		var count OperatorStatusCount
		if err := rows.Scan(&count.Status, &count.Count); err != nil {
			return nil, fmt.Errorf("store: scan operator %s count: %w", kind, err)
		}
		out = append(out, count)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate operator %s counts: %w", kind, err)
	}
	return out, nil
}

func readOperatorPlans(
	ctx context.Context,
	tx *sql.Tx,
	projectID, planID, afterID string,
	limit int,
) (OperatorPlanPage, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT p.id, p.slug, p.title, p.status,
		       SUM(
		         CASE
		           WHEN t.status IN ('done', 'skipped', 'decomposed') THEN 1
		           ELSE 0
		         END
		       ),
		       COUNT(t.id),
		       p.created_at, p.updated_at
		FROM plans p
		LEFT JOIN tasks t ON t.plan_id = p.id
		WHERE p.project_id = ?
		  AND (? = '' OR p.id = ?)
		  AND (? = '' OR p.id > ?)
		GROUP BY p.id, p.slug, p.title, p.status, p.created_at, p.updated_at
		ORDER BY p.id
		LIMIT ?
	`, projectID, planID, planID, afterID, afterID, limit+1)
	if err != nil {
		return OperatorPlanPage{}, fmt.Errorf("store: read operator plans: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]OperatorPlan, 0, limit+1)
	for rows.Next() {
		var plan OperatorPlan
		var createdRaw, updatedRaw string
		if err := rows.Scan(
			&plan.ID,
			&plan.Slug,
			&plan.Title,
			&plan.Status,
			&plan.TaskDone,
			&plan.TaskTotal,
			&createdRaw,
			&updatedRaw,
		); err != nil {
			return OperatorPlanPage{}, fmt.Errorf("store: scan operator plan: %w", err)
		}
		plan.CreatedAt, err = parseOperatorTimestamp("plan.created_at", createdRaw)
		if err != nil {
			return OperatorPlanPage{}, err
		}
		plan.UpdatedAt, err = parseOperatorTimestamp("plan.updated_at", updatedRaw)
		if err != nil {
			return OperatorPlanPage{}, err
		}
		items = append(items, plan)
	}
	if err := rows.Err(); err != nil {
		return OperatorPlanPage{}, fmt.Errorf("store: iterate operator plans: %w", err)
	}

	page := OperatorPlanPage{Items: items}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
		page.NextAfterID = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}

func readOperatorTasks(
	ctx context.Context,
	tx *sql.Tx,
	projectID, planID, taskID string,
	after OperatorTaskCursor,
	limit int,
) (OperatorTaskPage, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT t.plan_id, t.id, t.status, t.parallel_group, t.sequence_ordinal,
		       t.retry_count, t.reclaim_count, COALESCE(t.parent_task_id, ''),
		       COALESCE(t.claimed_by_worker_id, ''),
		       COALESCE(m.assigned_alias, ''), COALESCE(m.assigned_provider, ''),
		       COALESCE(m.assigned_model, ''), COALESCE(m.assigned_effort, ''),
		       COALESCE(m.assigned_independence_domain, ''),
		       COALESCE(m.group_path, ''), COALESCE(m.metadata_json, ''),
		       -- Does this task belong to a partition an operator can act on?
		       --
		       -- A RUNNING task always does: it was claimed AS a partition member,
		       -- so its grouping is a live fact, not a prediction. Applying the
		       -- dependency gate to it cleared every marker the instant a fan-out
		       -- turn was claimed -- during exactly the interval the marker
		       -- answers "one turn, or three independent workers?".
		       --
		       -- A not-yet-dispatched task belongs to one only if it is READY,
		       -- decided by the same NOT EXISTS walk over task_deps that
		       -- ReadyPartitions and ClaimNextReady use, so this cannot become a
		       -- second notion of ready.
		       (t.status = 'running'
		        OR (t.status IN ('pending','ready','blocked_capability','blocked_input')
		            AND NOT EXISTS (
		              SELECT 1 FROM task_deps d
		               JOIN tasks tdep ON tdep.plan_id = d.plan_id AND tdep.id = d.depends_on
		              WHERE d.plan_id = t.plan_id
		                AND d.task_id = t.id
		                AND tdep.status NOT IN ('done', 'skipped', 'decomposed')
		            ))) AS partitioned,
		       -- A dependency that can never be satisfied, or '' when none.
		       -- Deliberately NOT every unsatisfied dependency: an incomplete one
		       -- clears itself as upstream work finishes, so naming it would put
		       -- a scary marker on every healthy plan mid-flight. A failed one
		       -- never clears (nothing transitions out of 'failed'), which is
		       -- what makes the dependent's plain 'pending' badge a lie.
		       COALESCE((
		         SELECT d.depends_on FROM task_deps d
		          JOIN tasks tdep ON tdep.plan_id = d.plan_id AND tdep.id = d.depends_on
		         WHERE d.plan_id = t.plan_id
		           AND d.task_id = t.id
		           AND tdep.status = 'failed'
		         ORDER BY d.depends_on
		         LIMIT 1
		       ), '') AS blocked_by,
		       t.created_at, t.updated_at
		FROM tasks t
		JOIN plans p ON p.id = t.plan_id
		-- LEFT: metadata is written by a separate path, so an inner join would
		-- make a task with no metadata row DISAPPEAR from the operator's task
		-- list rather than show up with empty provenance. Silently dropping the
		-- task is far worse than reporting it unassigned.
		LEFT JOIN task_metadata m ON m.plan_id = t.plan_id AND m.task_id = t.id
		WHERE p.project_id = ?
		  AND (? = '' OR t.plan_id = ?)
		  AND (? = '' OR t.id = ?)
		  AND (
		    ? = ''
		    OR t.plan_id > ?
		    OR (t.plan_id = ? AND t.id > ?)
		  )
		ORDER BY t.plan_id, t.id
		LIMIT ?
	`,
		projectID,
		planID,
		planID,
		taskID,
		taskID,
		after.PlanID,
		after.PlanID,
		after.PlanID,
		after.TaskID,
		limit+1,
	)
	if err != nil {
		return OperatorTaskPage{}, fmt.Errorf("store: read operator tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]OperatorTask, 0, limit+1)
	for rows.Next() {
		var task OperatorTask
		var parallel, sequence sql.NullInt64
		var createdRaw, updatedRaw string
		var groupPath, metadataJSON string
		var partitioned bool
		if err := rows.Scan(
			&task.PlanID,
			&task.ID,
			&task.Status,
			&parallel,
			&sequence,
			&task.RetryCount,
			&task.ReclaimCount,
			&task.ParentTaskID,
			&task.ClaimedByWorkerID,
			&task.AssignedAlias,
			&task.AssignedProvider,
			&task.AssignedModel,
			&task.AssignedEffort,
			&task.AssignedIndependenceDomain,
			&groupPath,
			&metadataJSON,
			&partitioned,
			&task.BlockedByTaskID,
			&createdRaw,
			&updatedRaw,
		); err != nil {
			return OperatorTaskPage{}, fmt.Errorf("store: scan operator task: %w", err)
		}
		task.ParallelGroup = operatorNullableInt64(parallel)
		task.SequenceOrdinal = operatorNullableInt64(sequence)
		// Only a task that BELONGS to a partition gets an ordinal: one already
		// running as a member, or one ready to be dispatched as part of it. A
		// task still waiting on a dependency has no partition yet -- labelling
		// it anyway claimed a grouping dispatch would never perform.
		//
		// Found by DOGFOODING, not by a test: running Ralph on its own plan
		// showed `build` carrying the same marker as the three tasks declaring
		// after:[build]. The agreement test missed it because every task in its
		// fixture was independently ready, so agreement held vacuously.
		//
		// Derived through the SAME function ReadyPartitions groups by, so the
		// operator's view of "these run together" cannot drift from the rule
		// dispatch actually applies.
		if partitioned {
			task.PartitionOrdinal = readyPartitionOrdinal(ReadyPartition{
				GroupPath:  groupPath,
				BindingKey: declaredBindingKey(metadataJSON),
			})
		}
		task.CreatedAt, err = parseOperatorTimestamp("task.created_at", createdRaw)
		if err != nil {
			return OperatorTaskPage{}, err
		}
		task.UpdatedAt, err = parseOperatorTimestamp("task.updated_at", updatedRaw)
		if err != nil {
			return OperatorTaskPage{}, err
		}
		items = append(items, task)
	}
	if err := rows.Err(); err != nil {
		return OperatorTaskPage{}, fmt.Errorf("store: iterate operator tasks: %w", err)
	}

	page := OperatorTaskPage{Items: items}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextAfter = &OperatorTaskCursor{PlanID: last.PlanID, TaskID: last.ID}
	}
	return page, nil
}

func readOperatorWorkers(
	ctx context.Context,
	tx *sql.Tx,
	projectID string,
) ([]OperatorWorker, error) {
	rows, err := tx.QueryContext(ctx, `
		WITH project_claims AS (
		  SELECT t.claimed_by_worker_id AS worker_id, t.plan_id, t.id AS task_id, t.status
		  FROM tasks t
		  JOIN plans p ON p.id = t.plan_id
		  WHERE p.project_id = ? AND t.claimed_by_worker_id IS NOT NULL
		),
		project_worker_ids AS (
		  SELECT w.id
		  FROM workers w
		  JOIN plans p ON p.id = w.current_plan_id
		  WHERE w.status = 'running' AND p.project_id = ?
		  UNION
		  SELECT w.id
		  FROM workers w
		  JOIN project_claims c ON c.worker_id = w.id
		  WHERE w.status = 'running'
		)
		SELECT w.id, w.provider, COALESCE(w.model, ''), w.native_fanout, w.status,
		       w.started_at, w.last_heartbeat,
		       c.plan_id, c.task_id, c.status
		FROM project_worker_ids ids
		JOIN workers w ON w.id = ids.id
		LEFT JOIN project_claims c ON c.worker_id = w.id
		ORDER BY w.started_at, w.id, c.plan_id, c.task_id
		LIMIT ?
	`, projectID, projectID, MaxOperatorActiveWorkers+MaxOperatorActiveClaims+1)
	if err != nil {
		return nil, fmt.Errorf("store: read operator workers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	workers := make([]OperatorWorker, 0)
	workerIndex := make(map[string]int)
	claimCount := 0
	rowCount := 0
	for rows.Next() {
		rowCount++
		var (
			workerID, provider, model, status string
			nativeFanout                      bool
			startedRaw, heartbeatRaw          string
			claimPlan, claimTask, claimStatus sql.NullString
		)
		if err := rows.Scan(
			&workerID,
			&provider,
			&model,
			&nativeFanout,
			&status,
			&startedRaw,
			&heartbeatRaw,
			&claimPlan,
			&claimTask,
			&claimStatus,
		); err != nil {
			return nil, fmt.Errorf("store: scan operator worker: %w", err)
		}

		index, found := workerIndex[workerID]
		if !found {
			if len(workers) == MaxOperatorActiveWorkers {
				return nil, fmt.Errorf(
					"%w: more than %d active workers",
					ErrOperatorSnapshotTooLarge,
					MaxOperatorActiveWorkers,
				)
			}
			startedAt, parseErr := parseOperatorTimestamp("worker.started_at", startedRaw)
			if parseErr != nil {
				return nil, parseErr
			}
			lastHeartbeat, parseErr := parseOperatorTimestamp(
				"worker.last_heartbeat",
				heartbeatRaw,
			)
			if parseErr != nil {
				return nil, parseErr
			}
			index = len(workers)
			workerIndex[workerID] = index
			workers = append(workers, OperatorWorker{
				ID:            workerID,
				Provider:      provider,
				Model:         model,
				NativeFanout:  nativeFanout,
				Status:        status,
				StartedAt:     startedAt,
				LastHeartbeat: lastHeartbeat,
				Claims:        make([]OperatorWorkerClaim, 0),
			})
		}
		if claimPlan.Valid {
			if claimCount == MaxOperatorActiveClaims {
				return nil, fmt.Errorf(
					"%w: more than %d active worker claims",
					ErrOperatorSnapshotTooLarge,
					MaxOperatorActiveClaims,
				)
			}
			workers[index].Claims = append(workers[index].Claims, OperatorWorkerClaim{
				PlanID: claimPlan.String,
				TaskID: claimTask.String,
				Status: TaskStatus(claimStatus.String),
			})
			claimCount++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate operator workers: %w", err)
	}
	if rowCount == MaxOperatorActiveWorkers+MaxOperatorActiveClaims+1 {
		return nil, fmt.Errorf("%w: active worker row cap reached", ErrOperatorSnapshotTooLarge)
	}
	return workers, nil
}

func readOperatorEvents(
	ctx context.Context,
	tx *sql.Tx,
	projectID, planID, taskID string,
	beforeID int64,
	limit int,
) (OperatorEventPage, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT e.id, COALESCE(ep.id, ''),
		       CASE WHEN ep.id IS NULL THEN '' ELSE COALESCE(e.task_id, '') END,
		       e.kind, COALESCE(e.stream, ''), e.occurred_at,
		       -- The closed-set failure code only. json_extract on this ONE key
		       -- cannot surface free text from the rest of the payload.
		       COALESCE(json_extract(e.payload_json, '$.failure_category'), '')
		FROM events e
		LEFT JOIN plans ep ON ep.id = e.plan_id AND ep.project_id = ?
		WHERE (
		  e.project_id = ?
		  OR (
		    e.project_id IS NULL
		    AND e.plan_id IN (SELECT id FROM plans WHERE project_id = ?)
		  )
		)
		  AND (? = '' OR e.plan_id = ?)
		  AND (? = '' OR e.task_id = ?)
		  AND (? = 0 OR e.id < ?)
		ORDER BY e.id DESC
		LIMIT ?
	`,
		projectID,
		projectID,
		projectID,
		planID,
		planID,
		taskID,
		taskID,
		beforeID,
		beforeID,
		limit+1,
	)
	if err != nil {
		return OperatorEventPage{}, fmt.Errorf("store: read operator events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]OperatorEvent, 0, limit+1)
	for rows.Next() {
		var event OperatorEvent
		var occurredRaw string
		if err := rows.Scan(
			&event.ID,
			&event.PlanID,
			&event.TaskID,
			&event.Kind,
			&event.Stream,
			&occurredRaw,
			&event.FailureCategory,
		); err != nil {
			return OperatorEventPage{}, fmt.Errorf("store: scan operator event: %w", err)
		}
		event.OccurredAt, err = parseOperatorTimestamp("event.occurred_at", occurredRaw)
		if err != nil {
			return OperatorEventPage{}, err
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return OperatorEventPage{}, fmt.Errorf("store: iterate operator events: %w", err)
	}

	page := OperatorEventPage{Items: items}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
		page.NextBeforeID = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}

func readOperatorEventCursor(
	ctx context.Context,
	tx *sql.Tx,
	projectID string,
) (int64, error) {
	var cursor int64
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(id), 0)
		FROM events
		WHERE `+eventProjectScope,
		projectID,
		projectID,
	).Scan(&cursor)
	if err != nil {
		return 0, fmt.Errorf("store: read operator event cursor: %w", err)
	}
	return cursor, nil
}

func parseOperatorTimestamp(field, raw string) (time.Time, error) {
	parsed := parseDBTimestamp(raw)
	if parsed.IsZero() {
		return time.Time{}, fmt.Errorf(
			"store: operator query: invalid %s timestamp %q",
			field,
			raw,
		)
	}
	return parsed.UTC(), nil
}

func operatorNullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	out := value.Int64
	return &out
}
