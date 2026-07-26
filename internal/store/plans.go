package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PlanStatus enumerates the lifecycle states a plan can be in.
type PlanStatus string

// Plan lifecycle states.
const (
	PlanStatusDraft         PlanStatus = "draft"
	PlanStatusActive        PlanStatus = "active"
	PlanStatusPaused        PlanStatus = "paused"
	PlanStatusDone          PlanStatus = "done"
	PlanStatusFailedPartial PlanStatus = "failed_partial"
	PlanStatusArchived      PlanStatus = "archived"
	PlanStatusAbandoned     PlanStatus = "abandoned"
)

// Plan is the durable row, keyed by project (not repo_path — see §5b).
type Plan struct {
	ID             string
	ProjectID      string
	Slug           string
	Title          string
	Status         PlanStatus
	SourceMarkdown string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastSessionAt  sql.NullTime
	TagsJSON       string // JSON array
}

// CreatePlanOpts configures plan creation. The caller supplies slug + title;
// ID is generated via Store.uuid().
type CreatePlanOpts struct {
	ProjectID      string
	Slug           string
	Title          string
	SourceMarkdown string
	TagsJSON       string
}

// ErrDuplicateSlug is returned when a plan with (project_id, slug) already
// exists. Callers either pick a new slug or update.
var ErrDuplicateSlug = errors.New("store: plan with slug already exists in this project")

// ErrPlanNotFound is returned when an operation targets a plan id that no row
// matches. It is a typed sentinel so callers match with errors.Is rather than
// scraping the formatted message (the drive API maps it to CodeNotFound).
var ErrPlanNotFound = errors.New("store: plan not found")

// CreatePlan inserts a fresh plan in draft status and returns the newly
// generated UUID v7 id.
func (s *Store) CreatePlan(ctx context.Context, o CreatePlanOpts) (string, error) {
	if o.ProjectID == "" || o.Slug == "" || o.Title == "" {
		return "", fmt.Errorf("store: ProjectID, Slug, and Title required")
	}
	id := s.uuid()
	now := s.clock.Now().UTC().Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO plans(
			id, project_id, slug, title, status, source_markdown,
			created_at, updated_at, tags_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		id, o.ProjectID, o.Slug, o.Title,
		string(PlanStatusDraft), nullIfEmpty(o.SourceMarkdown),
		now, now, nullIfEmpty(o.TagsJSON),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return "", fmt.Errorf("%w: %s (project=%s)", ErrDuplicateSlug, o.Slug, o.ProjectID)
		}
		return "", fmt.Errorf("store: insert plan: %w", err)
	}
	return id, nil
}

// GetPlan loads a plan by id.
func (s *Store) GetPlan(ctx context.Context, id string) (*Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, slug, title, status, COALESCE(source_markdown,''),
		       created_at, updated_at, last_session_at, COALESCE(tags_json,'')
		FROM plans WHERE id = ?
	`, id)
	p, err := scanPlan(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: plan %q not found", id)
		}
		return nil, fmt.Errorf("store: get plan: %w", err)
	}
	return &p, nil
}

// SetPlanStatus updates the plan's status column. Activating a v2 plan also
// recomputes terminal convergence in the same transaction so resuming a plan
// whose tasks finished while it was paused cannot leave an impossible active
// plan behind.
func (s *Store) SetPlanStatus(ctx context.Context, id string, status PlanStatus) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin status update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `UPDATE plans SET status = ? WHERE id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("store: update status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update status rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %q", ErrPlanNotFound, id)
	}
	if status == PlanStatusActive {
		if err := convergeV2PlanTx(ctx, tx, id); err != nil {
			return fmt.Errorf("store: converge activated v2 plan: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit status update: %w", err)
	}
	return nil
}

// convergeV2PlanTx applies terminal convergence only to an active v2 plan.
// Lifecycle states owned by the operator (paused, abandoned, archived) are
// deliberately outside the UPDATE predicate and therefore survive late worker
// reports. A terminal failure wins over successful-terminal convergence.
func convergeV2PlanTx(ctx context.Context, tx *sql.Tx, planID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE plans
		SET status = CASE
		    WHEN EXISTS (
		      SELECT 1 FROM tasks t
		      WHERE t.plan_id = plans.id AND t.status = 'failed'
		    ) THEN 'failed_partial'
		    WHEN NOT EXISTS (
		      SELECT 1 FROM tasks t
		      WHERE t.plan_id = plans.id
		        AND t.status NOT IN ('done', 'skipped', 'decomposed')
		    ) THEN 'done'
		    ELSE 'active'
		  END
		WHERE id = ?
		  AND status = 'active'
		  AND EXISTS (
		    SELECT 1 FROM task_metadata m WHERE m.plan_id = plans.id
		  )
	`, planID)
	return err
}

// ListPlans returns plans for a project matching filter. Empty filter →
// active + paused. Empty projectID lists across all projects.
func (s *Store) ListPlans(ctx context.Context, projectID string, statuses []PlanStatus) ([]Plan, error) {
	if len(statuses) == 0 {
		statuses = []PlanStatus{PlanStatusActive, PlanStatusPaused}
	}
	args := make([]any, 0, len(statuses)+1)
	parts := make([]string, len(statuses))
	for i, st := range statuses {
		parts[i] = "?"
		args = append(args, string(st))
	}
	placeholders := strings.Join(parts, ",")

	where := "status IN (" + placeholders + ")"
	if projectID != "" {
		where += " AND project_id = ?"
		args = append(args, projectID)
	}

	//nolint:gosec // G201: placeholders is only "?" chars; args bind via QueryContext
	q := fmt.Sprintf(`
		SELECT id, project_id, slug, title, status, COALESCE(source_markdown,''),
		       created_at, updated_at, last_session_at, COALESCE(tags_json,'')
		FROM plans WHERE %s
		ORDER BY updated_at DESC
	`, where)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list plans: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type planScanner interface {
	Scan(dest ...any) error
}

func scanPlan(scanner planScanner) (Plan, error) {
	var p Plan
	var createdStr, updatedStr string
	err := scanner.Scan(
		&p.ID, &p.ProjectID, &p.Slug, &p.Title, &p.Status, &p.SourceMarkdown,
		&createdStr, &updatedStr, &p.LastSessionAt, &p.TagsJSON,
	)
	if err != nil {
		return Plan{}, err
	}
	p.CreatedAt = parseDBTimestamp(createdStr)
	p.UpdatedAt = parseDBTimestamp(updatedStr)
	return p, nil
}
