package plandag

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

// Plan is the durable row.
type Plan struct {
	ID             string
	Slug           string
	Title          string
	RepoPath       string
	RepoRemote     string
	Status         PlanStatus
	PrimaryVariant string
	Confidence     int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastSessionAt  sql.NullTime
	TagsJSON       string // JSON array
}

// CreatePlanOpts configures plan creation. The caller supplies slug +
// title; ID is generated via Store.uuid().
type CreatePlanOpts struct {
	Slug           string
	Title          string
	RepoPath       string
	RepoRemote     string
	PrimaryVariant string
	Confidence     int
	TagsJSON       string
}

// ErrDuplicateSlug is returned when a plan with (repo_path, slug)
// already exists. Callers either pick a new slug or update.
var ErrDuplicateSlug = errors.New("plandag: plan with slug already exists in this repo")

// CreatePlan inserts a fresh plan in draft status and returns the
// newly generated UUID v7 id.
func (s *Store) CreatePlan(ctx context.Context, o CreatePlanOpts) (string, error) {
	if o.Slug == "" || o.Title == "" {
		return "", fmt.Errorf("plandag: slug and title required")
	}
	id := s.uuid()
	now := s.clock.Now().UTC().Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO plans(
			id, slug, title, repo_path, repo_remote,
			status, primary_variant, confidence,
			created_at, updated_at, tags_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		id, o.Slug, o.Title, o.RepoPath, o.RepoRemote,
		string(PlanStatusDraft), o.PrimaryVariant, o.Confidence,
		now, now, o.TagsJSON,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return "", fmt.Errorf("%w: %s (repo=%s)", ErrDuplicateSlug, o.Slug, o.RepoPath)
		}
		return "", fmt.Errorf("plandag: insert plan: %w", err)
	}
	return id, nil
}

// GetPlan loads a plan by id.
func (s *Store) GetPlan(ctx context.Context, id string) (*Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, slug, title, COALESCE(repo_path,''), COALESCE(repo_remote,''),
		       status, COALESCE(primary_variant,''), COALESCE(confidence, 0),
		       created_at, updated_at, last_session_at, COALESCE(tags_json,'')
		FROM plans WHERE id = ?
	`, id)
	var p Plan
	var createdStr, updatedStr string
	if err := row.Scan(
		&p.ID, &p.Slug, &p.Title, &p.RepoPath, &p.RepoRemote,
		&p.Status, &p.PrimaryVariant, &p.Confidence,
		&createdStr, &updatedStr, &p.LastSessionAt, &p.TagsJSON,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("plandag: plan %q not found", id)
		}
		return nil, fmt.Errorf("plandag: get plan: %w", err)
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return &p, nil
}

// SetPlanStatus updates the plan's status column.
func (s *Store) SetPlanStatus(ctx context.Context, id string, status PlanStatus) error {
	res, err := s.db.ExecContext(ctx, `UPDATE plans SET status = ? WHERE id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("plandag: update status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("plandag: plan %q not found", id)
	}
	return nil
}

// ListPlans returns plans matching filter. Empty filter → active + paused.
func (s *Store) ListPlans(ctx context.Context, statuses []PlanStatus) ([]Plan, error) {
	if len(statuses) == 0 {
		statuses = []PlanStatus{PlanStatusActive, PlanStatusPaused}
	}
	args := make([]any, 0, len(statuses))
	parts := make([]string, len(statuses))
	for i, s := range statuses {
		parts[i] = "?"
		args = append(args, string(s))
	}
	placeholders := strings.Join(parts, ",")

	//nolint:gosec // G201: placeholders is only "?" chars; args bind via QueryContext
	q := fmt.Sprintf(`
		SELECT id, slug, title, COALESCE(repo_path,''), COALESCE(repo_remote,''),
		       status, COALESCE(primary_variant,''), COALESCE(confidence, 0),
		       created_at, updated_at, last_session_at, COALESCE(tags_json,'')
		FROM plans WHERE status IN (%s)
		ORDER BY updated_at DESC
	`, placeholders)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("plandag: list plans: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Plan
	for rows.Next() {
		var p Plan
		var createdStr, updatedStr string
		if err := rows.Scan(
			&p.ID, &p.Slug, &p.Title, &p.RepoPath, &p.RepoRemote,
			&p.Status, &p.PrimaryVariant, &p.Confidence,
			&createdStr, &updatedStr, &p.LastSessionAt, &p.TagsJSON,
		); err != nil {
			return nil, fmt.Errorf("plandag: scan: %w", err)
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		out = append(out, p)
	}
	return out, rows.Err()
}

// isUniqueViolation recognizes the modernc.org/sqlite UNIQUE error.
// Keeps the error matching in one place so callers can rely on
// ErrDuplicateSlug unconditionally.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// modernc.org/sqlite error text is stable across versions.
	return containsAny(err.Error(), "UNIQUE constraint", "constraint failed: UNIQUE")
}

// containsAny is a case-sensitive multi-needle substring check kept
// private because isUniqueViolation is the only caller.
func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		for i := 0; i+len(n) <= len(haystack); i++ {
			if haystack[i:i+len(n)] == n {
				return true
			}
		}
	}
	return false
}
