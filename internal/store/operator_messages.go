package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Operator-message page defaults and hard bounds.
const (
	DefaultOperatorMessageLimit = 100
	MaxOperatorMessageLimit     = 500
)

// OperatorMessageQuery selects a bounded chronological page of content-free
// A2A metadata. ProjectID is always required. PlanID optionally narrows to a
// plan; TaskID may only be supplied with PlanID.
type OperatorMessageQuery struct {
	ProjectID string `json:"project_id"`
	PlanID    string `json:"plan_id"`
	TaskID    string `json:"task_id"`
	AfterID   int64  `json:"after_id"`
	Limit     int    `json:"limit"`
}

// OperatorMessageMetadata is the safe envelope metadata for one durable A2A
// message. content_json is deliberately never selected, so raw worker
// evidence, provider output, prompts, commands, paths, or secrets cannot cross
// this store boundary.
type OperatorMessageMetadata struct {
	ID         int64     `json:"id"`
	WorkerID   string    `json:"worker_id"`
	PlanID     string    `json:"plan_id"`
	TaskID     string    `json:"task_id"`
	Role       string    `json:"role"`
	OccurredAt time.Time `json:"occurred_at"`
}

// OperatorMessagePage is one oldest-first A2A metadata page. NextAfterID is
// the monotonic keyset cursor for a subsequent call.
type OperatorMessagePage struct {
	Items       []OperatorMessageMetadata `json:"items"`
	HasMore     bool                      `json:"has_more"`
	NextAfterID int64                     `json:"next_after_id"`
}

// ListOperatorMessages returns bounded, chronological, content-free A2A
// metadata for one validated project/plan/task scope. It uses a read
// transaction so scope/cursor validation and the page read agree.
//
// Like ReadOperatorSnapshot, an error returns a nil page rather than a
// plausible empty page.
func (s *Store) ListOperatorMessages(
	ctx context.Context,
	q OperatorMessageQuery,
) (*OperatorMessagePage, error) {
	limit, err := validateOperatorMessageQuery(q)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("store: begin operator messages: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := readOperatorProject(ctx, tx, q.ProjectID); err != nil {
		return nil, err
	}
	if err := validateOperatorMessageScope(ctx, tx, q); err != nil {
		return nil, err
	}
	if err := validateOperatorMessageCursor(ctx, tx, q); err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT m.id, COALESCE(m.worker_id, ''), m.plan_id, m.task_id,
		       m.role, m.occurred_at
		FROM a2a_messages m
		JOIN plans p ON p.id = m.plan_id
		WHERE p.project_id = ?
		  AND (? = '' OR m.plan_id = ?)
		  AND (? = '' OR m.task_id = ?)
		  AND m.id > ?
		ORDER BY m.id
		LIMIT ?
	`,
		q.ProjectID,
		q.PlanID,
		q.PlanID,
		q.TaskID,
		q.TaskID,
		q.AfterID,
		limit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list operator messages: %w", err)
	}

	items, err := scanOperatorMessages(rows, limit)
	if err != nil {
		return nil, err
	}
	page := &OperatorMessagePage{Items: items}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
		page.NextAfterID = page.Items[len(page.Items)-1].ID
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit operator messages: %w", err)
	}
	return page, nil
}

func validateOperatorMessageQuery(q OperatorMessageQuery) (int, error) {
	if q.ProjectID == "" {
		return 0, fmt.Errorf("%w: project_id is required", ErrOperatorInvalidQuery)
	}
	if q.TaskID != "" && q.PlanID == "" {
		return 0, fmt.Errorf(
			"%w: task_id requires plan_id",
			ErrOperatorInvalidQuery,
		)
	}
	if q.AfterID < 0 {
		return 0, fmt.Errorf("%w: after_id must be non-negative", ErrOperatorInvalidQuery)
	}
	return normalizeOperatorLimit(
		"limit",
		q.Limit,
		DefaultOperatorMessageLimit,
		MaxOperatorMessageLimit,
	)
}

func validateOperatorMessageScope(
	ctx context.Context,
	tx *sql.Tx,
	q OperatorMessageQuery,
) error {
	if q.PlanID == "" {
		return nil
	}
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1 FROM plans WHERE project_id = ? AND id = ?
	`, q.ProjectID, q.PlanID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrOperatorPlanNotFound, q.PlanID)
	}
	if err != nil {
		return fmt.Errorf("store: validate operator message plan: %w", err)
	}
	if q.TaskID == "" {
		return nil
	}
	err = tx.QueryRowContext(ctx, `
		SELECT 1 FROM tasks WHERE plan_id = ? AND id = ?
	`, q.PlanID, q.TaskID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s/%s", ErrOperatorTaskNotFound, q.PlanID, q.TaskID)
	}
	if err != nil {
		return fmt.Errorf("store: validate operator message task: %w", err)
	}
	return nil
}

func validateOperatorMessageCursor(
	ctx context.Context,
	tx *sql.Tx,
	q OperatorMessageQuery,
) error {
	if q.AfterID == 0 {
		return nil
	}
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM a2a_messages m
		JOIN plans p ON p.id = m.plan_id
		WHERE m.id = ?
		  AND p.project_id = ?
		  AND (? = '' OR m.plan_id = ?)
		  AND (? = '' OR m.task_id = ?)
	`,
		q.AfterID,
		q.ProjectID,
		q.PlanID,
		q.PlanID,
		q.TaskID,
		q.TaskID,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: a2a message cursor", ErrOperatorInvalidCursor)
	}
	if err != nil {
		return fmt.Errorf("store: validate operator message cursor: %w", err)
	}
	return nil
}

func scanOperatorMessages(rows *sql.Rows, capacity int) ([]OperatorMessageMetadata, error) {
	defer func() { _ = rows.Close() }()

	items := make([]OperatorMessageMetadata, 0, capacity+1)
	for rows.Next() {
		var item OperatorMessageMetadata
		var occurredRaw string
		if err := rows.Scan(
			&item.ID,
			&item.WorkerID,
			&item.PlanID,
			&item.TaskID,
			&item.Role,
			&occurredRaw,
		); err != nil {
			return nil, fmt.Errorf("store: scan operator message: %w", err)
		}
		var err error
		item.OccurredAt, err = parseOperatorTimestamp("a2a_message.occurred_at", occurredRaw)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate operator messages: %w", err)
	}
	return items, nil
}
