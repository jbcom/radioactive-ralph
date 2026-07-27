package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// sortTeamRollups gives the operator views a stable order. Map iteration is
// randomized, so without this the TUI/GUI team list would reshuffle on refresh.
func sortTeamRollups(rollups []TeamRollup) {
	sort.Slice(rollups, func(i, j int) bool { return rollups[i].TeamPath < rollups[j].TeamPath })
}

// TaskExecutionMetadata is one task's durable scheduling and provenance record.
//
// It is deliberately NOT folded into Task. Task is the DAG node — the thing
// Ready and ClaimNextReady walk — and most callers only need that. Widening it
// would make every GetTask fire a second query for fields they ignore, so
// callers that want provenance ask for it explicitly.
type TaskExecutionMetadata struct {
	// GroupPath is the task's leaf-group identity as a dotted StepRef path
	// ("0.2"). Dispatch partitions a ready wave by this before native fan-out,
	// because fan-out delegates a whole partition to ONE provider under one
	// group heading — tasks from different leaf groups must not share one.
	GroupPath                  string
	TeamPath                   string
	MetadataJSON               string
	AssignedAlias              string
	AssignedProvider           string
	AssignedModel              string
	AssignedEffort             string
	AssignedIndependenceDomain string
	AssignedSessionID          string
	ProviderSessionID          string
	CalibrationID              string
	CapabilitySetJSON          string
	CompletionEvidenceJSON     string
	BlockedReason              string
}

// ErrTaskExecutionConflict reports a second provenance write from the current
// owner that disagrees with the immutable tuple already bound to its attempt.
var ErrTaskExecutionConflict = errors.New(
	"store: task execution provenance conflicts with the current owner")

// ErrTaskProviderSessionConflict reports a second provider-session write from
// the current owner that disagrees with the first session ID bound to the
// attempt.
var ErrTaskProviderSessionConflict = errors.New(
	"store: provider session conflicts with the current owner")

// ErrTaskMetadataMissing reports an operation that needs a task_metadata row
// against a task that has none. Migration 0003 does not backfill metadata for
// pre-existing tasks, so this is reachable rather than theoretical, and it fails
// closed: a caller that cannot record why a task is blocked must not report
// success after making it unclaimable.
var ErrTaskMetadataMissing = errors.New("store: task has no execution metadata")

// ErrDuplicateTaskMetadata reports a metadata row that already exists for this
// task.
//
// Typed rather than left as a raw driver error because the race is BENIGN and
// callers must be able to say so: two dispatchers materializing the same step,
// or a step whose plan was imported with its metadata already written. Matching
// on the driver's error text at the call site would couple every caller to
// SQLite's wording.
var ErrDuplicateTaskMetadata = errors.New("store: task metadata already exists")

// GetTaskExecutionMetadata loads one task's scheduling/provenance record.
func (s *Store) GetTaskExecutionMetadata(ctx context.Context, planID, taskID string) (TaskExecutionMetadata, error) {
	var metadata TaskExecutionMetadata
	err := s.db.QueryRowContext(ctx, `
		SELECT group_path, team_path, metadata_json, COALESCE(assigned_alias,''),
		       COALESCE(assigned_provider,''),
		       COALESCE(assigned_model,''), COALESCE(assigned_effort,''),
		       COALESCE(assigned_independence_domain,''),
		       COALESCE(assigned_session_id,''), COALESCE(provider_session_id,''),
		       COALESCE(calibration_id,''), COALESCE(capability_set_json,''),
		       COALESCE(completion_evidence_json,''), COALESCE(blocked_reason,'')
		FROM task_metadata WHERE plan_id = ? AND task_id = ?
	`, planID, taskID).Scan(
		&metadata.GroupPath, &metadata.TeamPath, &metadata.MetadataJSON, &metadata.AssignedAlias,
		&metadata.AssignedProvider, &metadata.AssignedModel, &metadata.AssignedEffort,
		&metadata.AssignedIndependenceDomain, &metadata.AssignedSessionID,
		&metadata.ProviderSessionID, &metadata.CalibrationID, &metadata.CapabilitySetJSON,
		&metadata.CompletionEvidenceJSON, &metadata.BlockedReason,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskExecutionMetadata{}, fmt.Errorf("%w: task %s", ErrTaskMetadataMissing, taskID)
		}
		return TaskExecutionMetadata{}, fmt.Errorf("store: get task execution metadata: %w", err)
	}
	return metadata, nil
}

// PutTaskMetadata inserts the immutable half of a task's metadata row. Plan
// import owns this; provenance fields are filled in later by the dispatch path.
func (s *Store) PutTaskMetadata(ctx context.Context, planID, taskID, groupPath, teamPath, metadataJSON string) error {
	return s.putTaskMetadataOn(ctx, s.db, planID, taskID, groupPath, teamPath, metadataJSON)
}

// putTaskMetadataOn inserts the row through any executor, so plan-graph import
// writes it inside the same transaction as the task it describes.
func (s *Store) putTaskMetadataOn(
	ctx context.Context,
	ex execer,
	planID, taskID, groupPath, teamPath, metadataJSON string,
) error {
	if groupPath == "" {
		return fmt.Errorf("store: group path required for task %s", taskID)
	}
	if _, err := ex.ExecContext(ctx, `
		INSERT INTO task_metadata(plan_id, task_id, group_path, team_path, metadata_json)
		VALUES (?, ?, ?, ?, ?)
	`, planID, taskID, groupPath, teamPath, metadataJSON); err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: task %s", ErrDuplicateTaskMetadata, taskID)
		}
		return fmt.Errorf("store: put task metadata %s: %w", taskID, err)
	}
	return nil
}

// ListTaskGroupPaths returns task id -> leaf-group path for one plan.
//
// Dispatch needs every ready task's group in one query rather than N lookups,
// and getting it from here rather than re-parsing the plan markdown is the
// point: recovering positional information by re-parsing is exactly the
// dependence the graph walk removes.
func (s *Store) ListTaskGroupPaths(ctx context.Context, planID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, group_path FROM task_metadata WHERE plan_id = ?
	`, planID)
	if err != nil {
		return nil, fmt.Errorf("store: list task group paths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var taskID, groupPath string
		if err := rows.Scan(&taskID, &groupPath); err != nil {
			return nil, fmt.Errorf("store: scan task group path: %w", err)
		}
		out[taskID] = groupPath
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate task group paths: %w", err)
	}
	return out, nil
}

// BindTaskCalibration snapshots the immutable calibration resolved for an
// await-calibration task. The first successful bind wins; idempotent repeats
// with the same content address are allowed, while a different address fails
// closed so an alias can never retarget an admitted task.
func (s *Store) BindTaskCalibration(
	ctx context.Context,
	planID, taskID, calibrationID, capabilitySetJSON string,
) error {
	if calibrationID == "" || capabilitySetJSON == "" {
		return fmt.Errorf("store: calibration id and capability set required")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE task_metadata
		SET calibration_id = ?, capability_set_json = ?
		WHERE plan_id = ? AND task_id = ?
		  AND (calibration_id IS NULL OR calibration_id = '')
	`, calibrationID, capabilitySetJSON, planID, taskID)
	if err != nil {
		return fmt.Errorf("store: bind task calibration: %w", err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: bind task calibration rows affected: %w", err)
	}
	if count == 1 {
		return nil
	}
	var existingID, existingCapabilities string
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(calibration_id,''), COALESCE(capability_set_json,'')
		FROM task_metadata WHERE plan_id = ? AND task_id = ?
	`, planID, taskID).Scan(&existingID, &existingCapabilities)
	if err != nil {
		// No row at all is not a rival binding — there is nothing to bind to.
		// Reporting it as a conflict would send a caller looking for a
		// competing calibration that does not exist.
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: task %s", ErrTaskMetadataMissing, taskID)
		}
		return fmt.Errorf("store: load task calibration binding: %w", err)
	}
	if existingID == calibrationID && existingCapabilities == capabilitySetJSON {
		return nil
	}
	return fmt.Errorf(
		"store: task calibration already bound to %q, cannot replace with %q",
		existingID, calibrationID,
	)
}

// RecordTaskExecution binds the selected provider request and worker session to
// the running task. The reporting session must still own the live claim so a
// late provenance write from a reclaimed worker cannot overwrite the current
// owner's immutable execution record.
func (s *Store) RecordTaskExecution(
	ctx context.Context,
	planID, taskID, alias, provider, model, effort, independenceDomain, sessionID string,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin task execution provenance: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE task_metadata
		SET assigned_alias = ?, assigned_provider = ?, assigned_model = ?,
		    assigned_effort = ?, assigned_independence_domain = ?,
		    provider_session_id = CASE
		      WHEN assigned_session_id = ? THEN provider_session_id
		      ELSE NULL
		    END,
		    assigned_session_id = ?, blocked_reason = NULL
		WHERE plan_id = ? AND task_id = ?
		  AND EXISTS (
		    SELECT 1 FROM tasks
		    WHERE plan_id = ? AND id = ? AND status = 'running'
		      AND claimed_by_session = ?
		  )
		  AND (
		    COALESCE(assigned_session_id, '') <> ?
		    OR (
		      COALESCE(assigned_alias, '') = ?
		      AND COALESCE(assigned_provider, '') = ?
		      AND COALESCE(assigned_model, '') = ?
		      AND COALESCE(assigned_effort, '') = ?
		      AND COALESCE(assigned_independence_domain, '') = ?
		      AND COALESCE(assigned_session_id, '') = ?
		    )
		  )
	`, alias, provider, model, effort, independenceDomain, sessionID, sessionID,
		planID, taskID, planID, taskID, sessionID,
		sessionID, alias, provider, model, effort, independenceDomain, sessionID)
	if err != nil {
		return fmt.Errorf("store: record task provider: %w", err)
	}
	if count, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: provider rows affected: %w", err)
	} else if count == 0 {
		var owner string
		err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(claimed_by_session, '')
			FROM tasks
			WHERE plan_id = ? AND id = ? AND status = 'running'
		`, planID, taskID).Scan(&owner)
		if err != nil || owner != sessionID {
			return ErrTaskNotOwnedRunning
		}
		// The caller DOES own the live claim, so the UPDATE matching nothing is
		// not automatically a rival write. Check for the other reachable cause
		// first: migration 0003 does not backfill metadata, so a pre-existing
		// task has no row to update. Calling that a conflict blames the caller
		// for a clash that never happened and hides the real fault — and a
		// caller retrying a "conflict" would retry forever.
		var exists int
		err = tx.QueryRowContext(ctx, `
			SELECT 1 FROM task_metadata WHERE plan_id = ? AND task_id = ?
		`, planID, taskID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: task %s", ErrTaskMetadataMissing, taskID)
		}
		if err != nil {
			return fmt.Errorf("store: check task metadata presence: %w", err)
		}
		return ErrTaskExecutionConflict
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit task execution provenance: %w", err)
	}
	return nil
}

// RecordTaskProviderSession stores the session identifier returned by the
// provider after a turn. Both the live task claim and the execution metadata
// must still identify claimingSessionID; otherwise this is a stale post-run
// write from a reclaimed worker.
func (s *Store) RecordTaskProviderSession(
	ctx context.Context,
	planID, taskID, claimingSessionID, providerSessionID string,
) error {
	storedProviderSessionID := nullIfEmpty(providerSessionID)
	res, err := s.db.ExecContext(ctx, `
		UPDATE task_metadata
		SET provider_session_id = ?
		WHERE plan_id = ? AND task_id = ?
		  AND assigned_session_id = ?
		  AND (
		    provider_session_id IS NULL
		    OR provider_session_id = ?
		  )
		  AND EXISTS (
		    SELECT 1 FROM tasks
		    WHERE plan_id = ? AND id = ? AND status = 'running'
		      AND claimed_by_session = ?
		  )
	`, storedProviderSessionID, planID, taskID, claimingSessionID,
		storedProviderSessionID,
		planID, taskID, claimingSessionID)
	if err != nil {
		return fmt.Errorf("store: record provider session: %w", err)
	}
	if count, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: provider session rows affected: %w", err)
	} else if count == 0 {
		var owner, assignedSession, currentProviderSession string
		err := s.db.QueryRowContext(ctx, `
			SELECT
			  COALESCE(t.claimed_by_session, ''),
			  COALESCE(m.assigned_session_id, ''),
			  COALESCE(m.provider_session_id, '')
			FROM tasks t
			JOIN task_metadata m
			  ON m.plan_id = t.plan_id AND m.task_id = t.id
			WHERE t.plan_id = ? AND t.id = ? AND t.status = 'running'
		`, planID, taskID).Scan(&owner, &assignedSession, &currentProviderSession)
		if err != nil || owner != claimingSessionID || assignedSession != claimingSessionID {
			return ErrTaskNotOwnedRunning
		}
		if currentProviderSession != providerSessionID {
			return ErrTaskProviderSessionConflict
		}
		return nil
	}
	return nil
}

// MarkBlockedCapability records a fail-closed pre-dispatch capability block and
// reports whether this call was the TRANSITION into that state.
//
// The transition is answered inside the same transaction as the write because
// it cannot be answered correctly outside it: dispatch evaluates the gate more
// than once per pass, so a caller comparing against a task row it read earlier
// sees a stale status and emits a duplicate. Callers that emit an event on
// blocking use this to emit once rather than on every supervisor tick.
func (s *Store) MarkBlockedCapability(ctx context.Context, planID, taskID, reason string) (bool, error) {
	return s.markMetadataBlocked(ctx, planID, taskID, TaskStatusBlockedCapability, reason)
}

// MarkBlockedInput records a fail-closed immutable-input admission failure.
// The bool reports whether this call was the transition into the blocked state;
// see MarkBlockedCapability.
func (s *Store) MarkBlockedInput(ctx context.Context, planID, taskID, reason string) (bool, error) {
	return s.markMetadataBlocked(ctx, planID, taskID, TaskStatusBlockedInput, reason)
}

// ClearTaskBlock returns a pre-dispatch-blocked task to pending and drops its
// recorded reason. Reports whether a row actually changed.
//
// Without this a block is a TRAP rather than a gate: ClaimTask accepts only
// pending or ready, so a task blocked on a capability stayed unclaimable even
// after an operator performed the exact fix the block asked for, and the plan
// never completed.
//
// Only the two fail-closed pre-dispatch states are cleared. A running, done, or
// failed task is untouched — those are owned by the worker lifecycle, and
// resetting one here would discard real execution state.
func (s *Store) ClearTaskBlock(ctx context.Context, planID, taskID string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("store: begin clear task block: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = 'pending'
		WHERE plan_id = ? AND id = ? AND status IN ('blocked_capability','blocked_input')
	`, planID, taskID)
	if err != nil {
		return false, fmt.Errorf("store: clear task block: %w", err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: clear task block rows affected: %w", err)
	}
	if count == 0 {
		// Not blocked: nothing to clear, and not an error — the caller checks
		// every eligible task on every pass.
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE task_metadata SET blocked_reason = '' WHERE plan_id = ? AND task_id = ?
	`, planID, taskID); err != nil {
		return false, fmt.Errorf("store: clear blocked reason: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("store: commit clear task block: %w", err)
	}
	return true, nil
}

func (s *Store) markMetadataBlocked(
	ctx context.Context,
	planID, taskID string,
	status TaskStatus,
	reason string,
) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("store: begin metadata block: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Read the prior status inside the transaction so "was it already blocked?"
	// is answered against the same snapshot the UPDATE below acts on.
	var priorStatus string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM tasks WHERE plan_id = ? AND id = ?`, planID, taskID,
	).Scan(&priorStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrTaskNotRunning
		}
		return false, fmt.Errorf("store: read prior task status: %w", err)
	}
	transitioned := priorStatus != string(status)

	res, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?
		-- blocked_input belongs here alongside blocked_capability: both are
		-- fail-closed pre-dispatch blocks, and a task already blocked on one
		-- cause must be re-markable when the other applies (or when the same
		-- one recurs with a new reason). Omitting it silently no-ops the
		-- status update and, with the RowsAffected check below, fails the call.
		WHERE plan_id = ? AND id = ? AND status IN ('pending','ready','blocked_capability','blocked_input')
	`, string(status), planID, taskID)
	if err != nil {
		return false, fmt.Errorf("store: mark metadata blocked: %w", err)
	}
	if count, err := res.RowsAffected(); err != nil {
		return false, fmt.Errorf("store: metadata block rows affected: %w", err)
	} else if count == 0 {
		return false, ErrTaskNotRunning
	}
	// A task with no task_metadata row must not be silently half-blocked.
	// Migration 0003 does not backfill metadata, so a task created before it
	// exists has none; without this check the status update above would commit
	// while the operator-facing reason was discarded, leaving a task that is
	// unclaimable for no visible cause.
	reasonRes, err := tx.ExecContext(ctx, `
		UPDATE task_metadata SET blocked_reason = ? WHERE plan_id = ? AND task_id = ?
	`, reason, planID, taskID)
	if err != nil {
		return false, fmt.Errorf("store: record capability reason: %w", err)
	}
	if count, err := reasonRes.RowsAffected(); err != nil {
		return false, fmt.Errorf("store: capability reason rows affected: %w", err)
	} else if count == 0 {
		return false, fmt.Errorf("%w: task %s has no metadata row to record a block reason",
			ErrTaskMetadataMissing, taskID)
	}
	// Emit only on the TRANSITION into the blocked state. Dispatch re-evaluates
	// the gate on every supervisor tick (and more than once per pass), so an
	// unconditional insert appends an identical event forever — burying the one
	// moment that matters, the block itself, under repeats of itself.
	if transitioned {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO events(plan_id, task_id, kind, stream, payload_json)
			VALUES (?, ?, ?, 'service', ?)
		`, planID, taskID, "task."+string(status), payloadJSON(EventPayload{Reason: reason})); err != nil {
			return false, fmt.Errorf("store: log metadata block: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("store: commit metadata block: %w", err)
	}
	return transitioned, nil
}

// TeamRollup aggregates task state per team path for the operator views.
type TeamRollup struct {
	TeamPath      string
	Total         int
	Pending       int
	Ready         int
	Running       int
	Done          int
	Blocked       int
	Failed        int
	ActiveWorkers int
	Providers     map[string]int
}

// TeamRollups summarizes task state by team path, optionally scoped to one
// project. This is an operator-facing query, not a hot path, so the join cost
// is fine here.
func (s *Store) TeamRollups(ctx context.Context, projectID string) ([]TeamRollup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.team_path, t.status,
		       COALESCE(NULLIF(m.assigned_alias,''), m.assigned_provider, ''),
		       COALESCE(t.claimed_by_worker_id, '')
		FROM task_metadata m
		JOIN tasks t ON t.plan_id = m.plan_id AND t.id = m.task_id
		JOIN plans p ON p.id = m.plan_id
		WHERE (? = '' OR p.project_id = ?)
	`, projectID, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: list team rollup source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byPath := map[string]*TeamRollup{}
	// ActiveWorkers counts DISTINCT workers, not running tasks. Under native
	// fan-out one worker legitimately claims several tasks in the same team
	// (ReclaimWorker in workers.go handles exactly that), so incrementing per
	// running row would report a worker holding five fan-out tasks as five
	// workers — and the never-block invariant is judged against worker counts.
	workersByPath := map[string]map[string]struct{}{}
	for rows.Next() {
		var teamPath, status, provider, workerID string
		if err := rows.Scan(&teamPath, &status, &provider, &workerID); err != nil {
			return nil, fmt.Errorf("store: scan team rollup: %w", err)
		}
		rollup, ok := byPath[teamPath]
		if !ok {
			rollup = &TeamRollup{TeamPath: teamPath, Providers: map[string]int{}}
			byPath[teamPath] = rollup
			workersByPath[teamPath] = map[string]struct{}{}
		}
		rollup.Total++
		switch TaskStatus(status) {
		case TaskStatusPending:
			rollup.Pending++
		case TaskStatusReady, TaskStatusReadyPendingApproval:
			rollup.Ready++
		case TaskStatusRunning:
			rollup.Running++
			if workerID != "" {
				workersByPath[teamPath][workerID] = struct{}{}
			}
		case TaskStatusDone:
			rollup.Done++
		case TaskStatusBlocked, TaskStatusBlockedCapability, TaskStatusBlockedInput:
			rollup.Blocked++
		case TaskStatusFailed:
			rollup.Failed++
		}
		if provider != "" {
			rollup.Providers[provider]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate team rollups: %w", err)
	}
	for path, workers := range workersByPath {
		byPath[path].ActiveWorkers = len(workers)
	}

	out := make([]TeamRollup, 0, len(byPath))
	for _, rollup := range byPath {
		out = append(out, *rollup)
	}
	sortTeamRollups(out)
	return out, nil
}
