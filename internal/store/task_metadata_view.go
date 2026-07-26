package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func (s *Store) enrichTaskMetadata(ctx context.Context, planID string, tasks []*Task) error {
	if len(tasks) == 0 {
		return nil
	}
	byID := make(map[string]*Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, team_path, COALESCE(assigned_alias,''),
		       COALESCE(assigned_provider,''),
		       COALESCE(assigned_model,''), COALESCE(assigned_effort,''),
		       COALESCE(assigned_independence_domain,''),
		       COALESCE(assigned_session_id,''), COALESCE(provider_session_id,''),
		       COALESCE(calibration_id,''), COALESCE(capability_set_json,''),
		       COALESCE(blocked_reason,''), COALESCE(completion_evidence_json,'')
		FROM task_metadata WHERE plan_id = ?
	`, planID)
	if err != nil {
		return fmt.Errorf("store: enrich task metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var metadata Task
		if err := rows.Scan(
			&id, &metadata.TeamPath, &metadata.AssignedAlias, &metadata.AssignedProvider,
			&metadata.AssignedModel, &metadata.AssignedEffort,
			&metadata.AssignedIndependenceDomain,
			&metadata.AssignedSessionID, &metadata.ProviderSessionID,
			&metadata.CalibrationID, &metadata.CapabilitySetJSON,
			&metadata.BlockedReason, &metadata.CompletionEvidenceJSON,
		); err != nil {
			return fmt.Errorf("store: scan task metadata view: %w", err)
		}
		if task := byID[id]; task != nil {
			task.TeamPath = metadata.TeamPath
			task.AssignedAlias = metadata.AssignedAlias
			task.AssignedProvider = metadata.AssignedProvider
			task.AssignedModel = metadata.AssignedModel
			task.AssignedEffort = metadata.AssignedEffort
			task.AssignedIndependenceDomain = metadata.AssignedIndependenceDomain
			task.AssignedSessionID = metadata.AssignedSessionID
			task.ProviderSessionID = metadata.ProviderSessionID
			task.CalibrationID = metadata.CalibrationID
			task.CapabilitySetJSON = metadata.CapabilitySetJSON
			task.BlockedReason = metadata.BlockedReason
			task.CompletionEvidenceJSON = metadata.CompletionEvidenceJSON
		}
	}
	return rows.Err()
}

// TeamRollup is one hierarchical team-prefix aggregate.
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

// TeamRollups aggregates every v2 task into each prefix of its team path.
func (s *Store) TeamRollups(ctx context.Context, projectID string) ([]TeamRollup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.team_path, t.status,
		       COALESCE(NULLIF(m.assigned_alias,''), m.assigned_provider, '')
		FROM task_metadata m
		JOIN tasks t ON t.plan_id = m.plan_id AND t.id = m.task_id
		JOIN plans p ON p.id = m.plan_id
		WHERE (? = '' OR p.project_id = ?)
	`, projectID, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: list team rollup source: %w", err)
	}
	defer func() { _ = rows.Close() }()
	rollups := map[string]*TeamRollup{}
	for rows.Next() {
		var teamPath, status, provider string
		if err := rows.Scan(&teamPath, &status, &provider); err != nil {
			return nil, fmt.Errorf("store: scan team rollup source: %w", err)
		}
		parts := strings.Split(teamPath, "/")
		for i := range parts {
			prefix := strings.Join(parts[:i+1], "/")
			rollup := rollups[prefix]
			if rollup == nil {
				rollup = &TeamRollup{TeamPath: prefix, Providers: map[string]int{}}
				rollups[prefix] = rollup
			}
			applyTeamStatus(rollup, TaskStatus(status))
			if provider != "" {
				rollup.Providers[provider]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate team rollup source: %w", err)
	}
	out := make([]TeamRollup, 0, len(rollups))
	for _, rollup := range rollups {
		out = append(out, *rollup)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TeamPath < out[j].TeamPath })
	return out, nil
}

func applyTeamStatus(rollup *TeamRollup, status TaskStatus) {
	rollup.Total++
	switch status {
	case TaskStatusPending:
		rollup.Pending++
	case TaskStatusReady, TaskStatusReadyPendingApproval:
		rollup.Ready++
	case TaskStatusRunning:
		rollup.Running++
		rollup.ActiveWorkers++
	case TaskStatusDone, TaskStatusSkipped, TaskStatusDecomposed:
		rollup.Done++
	case TaskStatusBlocked, TaskStatusBlockedCapability, TaskStatusBlockedInput:
		rollup.Blocked++
	case TaskStatusFailed:
		rollup.Failed++
	}
}
