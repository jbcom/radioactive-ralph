package plandag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RepoTaskSummary is a task plus its repo-local plan context and latest event.
type RepoTaskSummary struct {
	PlanID            string
	PlanSlug          string
	PlanTitle         string
	PlanStatus        PlanStatus
	Task              Task
	LatestEventType   string
	LatestPayloadJSON string
}

// SessionVariantSummary is the current runtime-facing view of one active
// session_variant row joined to task + plan metadata.
type SessionVariantSummary struct {
	ID            string
	SessionID     string
	VariantName   string
	Status        string
	PlanID        string
	PlanSlug      string
	TaskID        string
	TaskDesc      string
	StartedAt     time.Time
	LastHeartbeat time.Time
}

// ListRepoTaskSummaries returns tasks for one repo with enough plan/event
// context for operator UIs.
func (s *Store) ListRepoTaskSummaries(ctx context.Context, repoPath string, statuses []TaskStatus, limit int) ([]RepoTaskSummary, error) {
	query := `
		SELECT
		  plans.id,
		  plans.slug,
		  plans.title,
		  plans.status,
		  tasks.id, tasks.plan_id, tasks.description, COALESCE(tasks.complexity,''), COALESCE(tasks.effort,''),
		  COALESCE(tasks.variant_hint,''), tasks.context_boundary, COALESCE(tasks.acceptance_json,''),
		  tasks.status, COALESCE(tasks.assigned_variant,''), COALESCE(tasks.claimed_by_session,''),
		  COALESCE(tasks.claimed_by_variant_id,''), tasks.retry_count, tasks.reclaim_count,
		  COALESCE(tasks.parent_task_id,''), tasks.created_at, tasks.updated_at,
		  COALESCE((
		    SELECT event_type FROM task_events
		    WHERE plan_id = tasks.plan_id AND task_id = tasks.id
		    ORDER BY occurred_at DESC, id DESC
		    LIMIT 1
		  ), ''),
		  COALESCE((
		    SELECT payload_json FROM task_events
		    WHERE plan_id = tasks.plan_id AND task_id = tasks.id
		    ORDER BY occurred_at DESC, id DESC
		    LIMIT 1
		  ), '')
		FROM tasks
		JOIN plans ON plans.id = tasks.plan_id
		WHERE plans.repo_path = ?
	`
	args := []any{repoPath}
	if len(statuses) > 0 {
		//nolint:gosec // placeholders is generated entirely from '?' tokens
		query += ` AND tasks.status IN (` + statusPlaceholders(len(statuses)) + `)`
		for _, status := range statuses {
			args = append(args, string(status))
		}
	}
	query += `
		ORDER BY
		  CASE tasks.status
		    WHEN 'ready_pending_approval' THEN 0
		    WHEN 'blocked' THEN 1
		    WHEN 'running' THEN 2
		    WHEN 'pending' THEN 3
		    ELSE 4
		  END,
		  plans.updated_at DESC,
		  tasks.updated_at DESC
	`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("plandag: list repo task summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RepoTaskSummary
	for rows.Next() {
		var item RepoTaskSummary
		var cb int
		var createdStr, updatedStr string
		if err := rows.Scan(
			&item.PlanID, &item.PlanSlug, &item.PlanTitle, &item.PlanStatus,
			&item.Task.ID, &item.Task.PlanID, &item.Task.Description, &item.Task.Complexity, &item.Task.Effort,
			&item.Task.VariantHint, &cb, &item.Task.AcceptanceJSON,
			&item.Task.Status, &item.Task.AssignedVariant, &item.Task.ClaimedBySession,
			&item.Task.ClaimedByVariantID, &item.Task.RetryCount, &item.Task.ReclaimCount,
			&item.Task.ParentTaskID, &createdStr, &updatedStr,
			&item.LatestEventType, &item.LatestPayloadJSON,
		); err != nil {
			return nil, fmt.Errorf("plandag: scan repo task summary: %w", err)
		}
		item.Task.ContextBoundary = cb == 1
		item.Task.CreatedAt = parseDBTimestamp(createdStr)
		item.Task.UpdatedAt = parseDBTimestamp(updatedStr)
		out = append(out, item)
	}
	return out, rows.Err()
}

// ListActiveSessionVariants returns running/idle session variants for a repo.
func (s *Store) ListActiveSessionVariants(ctx context.Context, repoPath string, limit int) ([]SessionVariantSummary, error) {
	query := `
		SELECT
		  sv.id,
		  sv.session_id,
		  sv.variant_name,
		  sv.status,
		  COALESCE(sv.current_plan_id, ''),
		  COALESCE(plans.slug, ''),
		  COALESCE(sv.current_task_id, ''),
		  COALESCE(tasks.description, ''),
		  sv.started_at,
		  sv.last_heartbeat
		FROM session_variants sv
		LEFT JOIN plans ON plans.id = sv.current_plan_id
		LEFT JOIN tasks ON tasks.plan_id = sv.current_plan_id AND tasks.id = sv.current_task_id
		WHERE (plans.repo_path = ? OR (plans.repo_path IS NULL AND ? = ''))
		  AND sv.status IN ('running', 'idle')
		ORDER BY sv.last_heartbeat DESC, sv.started_at DESC
	`
	args := []any{repoPath, repoPath}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("plandag: list active session variants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SessionVariantSummary
	for rows.Next() {
		var item SessionVariantSummary
		var startedStr, heartbeatStr string
		if err := rows.Scan(
			&item.ID,
			&item.SessionID,
			&item.VariantName,
			&item.Status,
			&item.PlanID,
			&item.PlanSlug,
			&item.TaskID,
			&item.TaskDesc,
			&startedStr,
			&heartbeatStr,
		); err != nil {
			return nil, fmt.Errorf("plandag: scan active session variant: %w", err)
		}
		item.StartedAt = parseDBTimestamp(startedStr)
		item.LastHeartbeat = parseDBTimestamp(heartbeatStr)
		out = append(out, item)
	}
	return out, rows.Err()
}

// ParseTaskPayload decodes one payload_json string into the typed helper.
func ParseTaskPayload(raw string) (TaskEventPayload, error) {
	var payload TaskEventPayload
	if strings.TrimSpace(raw) == "" || raw == "{}" {
		return payload, nil
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return TaskEventPayload{}, err
	}
	return payload, nil
}
