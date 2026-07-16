package store

import (
	"context"
	"fmt"
)

// RecordSpendOpts configures RecordSpend.
type RecordSpendOpts struct {
	ProjectID    string
	WorkerID     string // optional
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	CachedTokens int
	CostUSD      float64
}

// RecordSpend appends one spend row for a completed provider turn. Used by
// the orchestrator to accumulate provider.Usage.CostUSD per project so
// spend caps (configured per provider/variant) can be enforced before the
// next dispatch.
func (s *Store) RecordSpend(ctx context.Context, o RecordSpendOpts) error {
	if o.ProjectID == "" || o.Provider == "" {
		return fmt.Errorf("store: ProjectID and Provider required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO spend(
			project_id, worker_id, provider, model,
			input_tokens, output_tokens, cached_tokens, cost_usd
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, o.ProjectID, nullIfEmpty(o.WorkerID), o.Provider, nullIfEmpty(o.Model),
		o.InputTokens, o.OutputTokens, o.CachedTokens, o.CostUSD)
	if err != nil {
		return fmt.Errorf("store: record spend: %w", err)
	}
	return nil
}

// ProjectSpendByProvider sums cost_usd for one project, grouped by
// provider. Used to enforce a per-provider spend cap before dispatch.
func (s *Store) ProjectSpendByProvider(ctx context.Context, projectID string) (map[string]float64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, COALESCE(SUM(cost_usd), 0)
		FROM spend
		WHERE project_id = ?
		GROUP BY provider
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: sum project spend: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]float64{}
	for rows.Next() {
		var provider string
		var total float64
		if err := rows.Scan(&provider, &total); err != nil {
			return nil, fmt.Errorf("store: scan project spend: %w", err)
		}
		out[provider] = total
	}
	return out, rows.Err()
}
