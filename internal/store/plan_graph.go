package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// OutputReservation is one statically exclusive project-relative output path.
type OutputReservation struct {
	Path string
	Mode string
}

// InputReservation is one shared-read project-relative content pin.
type InputReservation struct {
	Path   string
	SHA256 string
}

// GraphTaskSpec is one fully validated ralph.plan/v2 task.
type GraphTaskSpec struct {
	ID                string
	Description       string
	TeamPath          string
	MetadataJSON      string
	AcceptanceJSON    string
	CalibrationID     string
	CapabilitySetJSON string
	DependsOn         []string
	Inputs            []InputReservation
	Outputs           []OutputReservation
	RequiresApproval  bool
	Order             int
}

// CreatePlanGraphOpts atomically creates a plan and its explicit task DAG.
type CreatePlanGraphOpts struct {
	Plan   CreatePlanOpts
	Status PlanStatus
	Tasks  []GraphTaskSpec
}

// TaskExecutionMetadata is the durable v2 scheduling/provenance record.
type TaskExecutionMetadata struct {
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

// CreatePlanGraph persists the plan, stable tasks, metadata, dependencies, and
// output reservations in one transaction. Any malformed edge rolls back all
// rows, including the plan.
func (s *Store) CreatePlanGraph(ctx context.Context, opts CreatePlanGraphOpts) (string, error) {
	if opts.Plan.ProjectID == "" || opts.Plan.Slug == "" || opts.Plan.Title == "" {
		return "", fmt.Errorf("store: ProjectID, Slug, and Title required")
	}
	if len(opts.Tasks) == 0 {
		return "", fmt.Errorf("store: plan graph requires tasks")
	}
	if err := validateGraphSpecs(opts.Tasks); err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("store: begin plan graph: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	planID := s.uuid()
	now := s.clock.Now().UTC().Format(time.RFC3339)
	status := opts.Status
	if status == "" {
		status = PlanStatusDraft
	}
	if err := insertGraphPlan(ctx, tx, planID, now, status, opts.Plan); err != nil {
		return "", err
	}
	for _, task := range opts.Tasks {
		if err := insertGraphTask(ctx, tx, planID, task); err != nil {
			return "", err
		}
	}
	for _, task := range opts.Tasks {
		for _, dependency := range task.DependsOn {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO task_deps(plan_id, task_id, depends_on) VALUES (?, ?, ?)`,
				planID, task.ID, dependency); err != nil {
				return "", fmt.Errorf("store: insert task dependency %s -> %s: %w", task.ID, dependency, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("store: commit plan graph: %w", err)
	}
	return planID, nil
}

func insertGraphPlan(ctx context.Context, tx *sql.Tx, id, now string, status PlanStatus, opts CreatePlanOpts) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO plans(
			id, project_id, slug, title, status, source_markdown,
			created_at, updated_at, tags_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, opts.ProjectID, opts.Slug, opts.Title, string(status), nullIfEmpty(opts.SourceMarkdown),
		now, now, nullIfEmpty(opts.TagsJSON))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: %s (project=%s)", ErrDuplicateSlug, opts.Slug, opts.ProjectID)
		}
		return fmt.Errorf("store: insert graph plan: %w", err)
	}
	return nil
}

func insertGraphTask(ctx context.Context, tx *sql.Tx, planID string, task GraphTaskSpec) error {
	if task.ID == "" || task.Description == "" || task.TeamPath == "" || task.MetadataJSON == "" {
		return fmt.Errorf("store: graph task id, description, team, and metadata required")
	}
	status := string(TaskStatusPending)
	if task.RequiresApproval {
		status = string(TaskStatusReadyPendingApproval)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tasks(id, plan_id, description, status, sequence_ordinal, acceptance_json)
		VALUES (?, ?, ?, ?, ?, ?)
	`, task.ID, planID, task.Description, status, task.Order, nullIfEmpty(task.AcceptanceJSON)); err != nil {
		return fmt.Errorf("store: insert graph task %s: %w", task.ID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO task_metadata(
			plan_id, task_id, team_path, metadata_json, calibration_id, capability_set_json
		) VALUES (?, ?, ?, ?, ?, ?)
	`, planID, task.ID, task.TeamPath, task.MetadataJSON,
		nullIfEmpty(task.CalibrationID), nullIfEmpty(task.CapabilitySetJSON)); err != nil {
		return fmt.Errorf("store: insert task metadata %s: %w", task.ID, err)
	}
	for _, input := range task.Inputs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO task_input_reservations(plan_id, task_id, path, sha256)
			VALUES (?, ?, ?, ?)
		`, planID, task.ID, input.Path, input.SHA256); err != nil {
			return fmt.Errorf("store: reserve task input %s:%s: %w", task.ID, input.Path, err)
		}
	}
	for _, output := range task.Outputs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO task_output_reservations(plan_id, task_id, path, mode)
			VALUES (?, ?, ?, ?)
		`, planID, task.ID, output.Path, output.Mode); err != nil {
			return fmt.Errorf("store: reserve task output %s:%s: %w", task.ID, output.Path, err)
		}
	}
	return nil
}

// GetTaskExecutionMetadata loads one v2 task's scheduling/provenance record.
func (s *Store) GetTaskExecutionMetadata(ctx context.Context, planID, taskID string) (TaskExecutionMetadata, error) {
	var metadata TaskExecutionMetadata
	err := s.db.QueryRowContext(ctx, `
		SELECT team_path, metadata_json, COALESCE(assigned_alias,''),
		       COALESCE(assigned_provider,''),
		       COALESCE(assigned_model,''), COALESCE(assigned_effort,''),
		       COALESCE(assigned_independence_domain,''),
		       COALESCE(assigned_session_id,''), COALESCE(provider_session_id,''),
		       COALESCE(calibration_id,''), COALESCE(capability_set_json,''),
		       COALESCE(completion_evidence_json,''), COALESCE(blocked_reason,'')
		FROM task_metadata WHERE plan_id = ? AND task_id = ?
	`, planID, taskID).Scan(
		&metadata.TeamPath, &metadata.MetadataJSON, &metadata.AssignedAlias,
		&metadata.AssignedProvider, &metadata.AssignedModel, &metadata.AssignedEffort,
		&metadata.AssignedIndependenceDomain, &metadata.AssignedSessionID,
		&metadata.ProviderSessionID, &metadata.CalibrationID, &metadata.CapabilitySetJSON,
		&metadata.CompletionEvidenceJSON, &metadata.BlockedReason,
	)
	if err != nil {
		return TaskExecutionMetadata{}, fmt.Errorf("store: get task execution metadata: %w", err)
	}
	return metadata, nil
}

// ErrTaskNotRunning reports a provenance update attempted outside a live claim.
var ErrTaskNotRunning = errors.New("store: task is not running")

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
// the running task.
func (s *Store) RecordTaskExecution(
	ctx context.Context,
	planID, taskID, alias, provider, model, effort, independenceDomain, sessionID string,
) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE task_metadata
		SET assigned_alias = ?, assigned_provider = ?, assigned_model = ?,
		    assigned_effort = ?, assigned_independence_domain = ?,
		    assigned_session_id = ?, blocked_reason = NULL
		WHERE plan_id = ? AND task_id = ?
		  AND EXISTS (
		    SELECT 1 FROM tasks
		    WHERE plan_id = ? AND id = ? AND status = 'running'
		  )
	`, alias, provider, model, effort, independenceDomain, sessionID,
		planID, taskID, planID, taskID)
	if err != nil {
		return fmt.Errorf("store: record task provider: %w", err)
	}
	if count, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: provider rows affected: %w", err)
	} else if count == 0 {
		return ErrTaskNotRunning
	}
	return nil
}

// RecordTaskProvider preserves the original provider-only write API.
func (s *Store) RecordTaskProvider(ctx context.Context, planID, taskID, provider string) error {
	return s.RecordTaskExecution(ctx, planID, taskID, provider, provider, "", "", "", "")
}

// RecordTaskProviderSession stores the session identifier returned by the
// provider after a turn.
func (s *Store) RecordTaskProviderSession(
	ctx context.Context,
	planID, taskID, providerSessionID string,
) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE task_metadata SET provider_session_id = ?
		WHERE plan_id = ? AND task_id = ?
	`, nullIfEmpty(providerSessionID), planID, taskID)
	if err != nil {
		return fmt.Errorf("store: record provider session: %w", err)
	}
	return nil
}

// MarkBlockedCapability records a fail-closed pre-dispatch capability block.
func (s *Store) MarkBlockedCapability(ctx context.Context, planID, taskID, reason string) error {
	return s.markMetadataBlocked(ctx, planID, taskID, TaskStatusBlockedCapability, reason)
}

// MarkBlockedInput records a fail-closed immutable-input admission failure.
func (s *Store) MarkBlockedInput(ctx context.Context, planID, taskID, reason string) error {
	return s.markMetadataBlocked(ctx, planID, taskID, TaskStatusBlockedInput, reason)
}

func (s *Store) markMetadataBlocked(
	ctx context.Context,
	planID, taskID string,
	status TaskStatus,
	reason string,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin metadata block: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?
		WHERE plan_id = ? AND id = ? AND status IN ('pending','ready','blocked_capability')
	`, string(status), planID, taskID)
	if err != nil {
		return fmt.Errorf("store: mark metadata blocked: %w", err)
	}
	if count, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: metadata block rows affected: %w", err)
	} else if count == 0 {
		return ErrTaskNotRunning
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE task_metadata SET blocked_reason = ? WHERE plan_id = ? AND task_id = ?
	`, reason, planID, taskID); err != nil {
		return fmt.Errorf("store: record capability reason: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events(plan_id, task_id, kind, stream, payload_json)
		VALUES (?, ?, ?, 'service', ?)
	`, planID, taskID, "task."+string(status), payloadJSON(EventPayload{Reason: reason})); err != nil {
		return fmt.Errorf("store: log metadata block: %w", err)
	}
	return tx.Commit()
}
