package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ProviderCalibration is immutable evidence-backed provider capability data.
type ProviderCalibration struct {
	ID                 string
	Alias              string
	Provider           string
	Model              string
	Effort             string
	BinaryPath         string
	BinaryVersion      string
	BinarySHA256       string
	InvocationHash     string
	InferenceDomain    string
	ControlDomain      string
	IndependenceDomain string
	ModelDigest        string
	Capabilities       []string
	EvidenceJSON       string
}

// PutProviderCalibration validates, content-addresses, and durably stores one
// calibration result. Identical content is idempotent.
func (s *Store) PutProviderCalibration(ctx context.Context, value ProviderCalibration) (string, error) {
	value, capabilitiesJSON, id, err := prepareProviderCalibration(value)
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("store: begin provider calibration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO provider_calibrations(
			id, alias, provider, model, effort, binary_path, binary_version,
			binary_sha256, invocation_hash, inference_domain, control_domain,
			independence_domain, model_digest, capabilities_json, evidence_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, id, value.Alias, value.Provider, value.Model, value.Effort,
		value.BinaryPath, value.BinaryVersion, value.BinarySHA256,
		value.InvocationHash, value.InferenceDomain, value.ControlDomain,
		value.IndependenceDomain, nullIfEmpty(value.ModelDigest),
		string(capabilitiesJSON), value.EvidenceJSON)
	if err != nil {
		return "", fmt.Errorf("store: put provider calibration: %w", err)
	}
	if err := readmitCalibrationWaiters(ctx, tx, value.Alias); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("store: commit provider calibration: %w", err)
	}
	return id, nil
}

func prepareProviderCalibration(
	value ProviderCalibration,
) (ProviderCalibration, []byte, string, error) {
	value.Alias = strings.TrimSpace(value.Alias)
	value.Provider = strings.TrimSpace(value.Provider)
	value.Model = strings.TrimSpace(value.Model)
	value.Effort = strings.TrimSpace(value.Effort)
	value.BinaryPath = strings.TrimSpace(value.BinaryPath)
	value.BinaryVersion = strings.TrimSpace(value.BinaryVersion)
	value.BinarySHA256 = strings.TrimSpace(value.BinarySHA256)
	value.InvocationHash = strings.TrimSpace(value.InvocationHash)
	value.InferenceDomain = strings.TrimSpace(value.InferenceDomain)
	value.ControlDomain = strings.TrimSpace(value.ControlDomain)
	value.IndependenceDomain = strings.TrimSpace(value.IndependenceDomain)
	value.ModelDigest = strings.TrimSpace(value.ModelDigest)
	if value.Alias == "" || value.Provider == "" || value.Model == "" || value.Effort == "" ||
		value.BinaryPath == "" || value.BinaryVersion == "" ||
		value.BinarySHA256 == "" || value.InvocationHash == "" ||
		value.InferenceDomain == "" || value.ControlDomain == "" ||
		value.IndependenceDomain == "" ||
		strings.TrimSpace(value.EvidenceJSON) == "" || len(value.Capabilities) == 0 {
		return ProviderCalibration{}, nil, "", fmt.Errorf("store: complete calibration identity, binary, domains, capabilities, and evidence required")
	}
	for label, hash := range map[string]string{
		"binary sha256":   value.BinarySHA256,
		"invocation hash": value.InvocationHash,
	} {
		if len(hash) != 64 {
			return ProviderCalibration{}, nil, "", fmt.Errorf("store: calibration %s must be 64 lowercase hex characters", label)
		}
		for _, char := range hash {
			if !strings.ContainsRune("0123456789abcdef", char) {
				return ProviderCalibration{}, nil, "", fmt.Errorf("store: calibration %s must be 64 lowercase hex characters", label)
			}
		}
	}
	if !json.Valid([]byte(value.EvidenceJSON)) {
		return ProviderCalibration{}, nil, "", fmt.Errorf("store: calibration evidence must be valid JSON")
	}
	capabilities := append([]string{}, value.Capabilities...)
	slices.Sort(capabilities)
	capabilities = slices.Compact(capabilities)
	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == "" {
			return ProviderCalibration{}, nil, "", fmt.Errorf("store: calibration capability must be nonempty")
		}
	}
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return ProviderCalibration{}, nil, "", fmt.Errorf("store: marshal calibration capabilities: %w", err)
	}
	value.Capabilities = capabilities
	canonical, err := json.Marshal(struct {
		Alias              string          `json:"alias"`
		Provider           string          `json:"provider"`
		Model              string          `json:"model"`
		Effort             string          `json:"effort"`
		BinaryPath         string          `json:"binary_path"`
		BinaryVersion      string          `json:"binary_version"`
		BinarySHA256       string          `json:"binary_sha256"`
		InvocationHash     string          `json:"invocation_hash"`
		InferenceDomain    string          `json:"inference_domain"`
		ControlDomain      string          `json:"control_domain"`
		IndependenceDomain string          `json:"independence_domain"`
		ModelDigest        string          `json:"model_digest,omitempty"`
		Capabilities       json.RawMessage `json:"capabilities"`
		Evidence           json.RawMessage `json:"evidence"`
	}{
		Alias: value.Alias, Provider: value.Provider, Model: value.Model, Effort: value.Effort,
		BinaryPath: value.BinaryPath, BinaryVersion: value.BinaryVersion,
		BinarySHA256: value.BinarySHA256, InvocationHash: value.InvocationHash,
		InferenceDomain: value.InferenceDomain, ControlDomain: value.ControlDomain,
		IndependenceDomain: value.IndependenceDomain, ModelDigest: value.ModelDigest,
		Capabilities: capabilitiesJSON, Evidence: json.RawMessage(value.EvidenceJSON),
	})
	if err != nil {
		return ProviderCalibration{}, nil, "", fmt.Errorf("store: marshal calibration: %w", err)
	}
	id := fmt.Sprintf("sha256:%x", sha256.Sum256(canonical))
	return value, capabilitiesJSON, id, nil
}

func readmitCalibrationWaiters(ctx context.Context, tx *sql.Tx, alias string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT m.plan_id, m.task_id, m.metadata_json
		FROM task_metadata m
		JOIN tasks t ON t.plan_id = m.plan_id AND t.id = m.task_id
		WHERE t.status = 'blocked_capability'
	`)
	if err != nil {
		return fmt.Errorf("store: list calibration waiters: %w", err)
	}
	type waiter struct {
		planID string
		taskID string
	}
	var waiters []waiter
	for rows.Next() {
		var planID, taskID, metadataJSON string
		if err := rows.Scan(&planID, &taskID, &metadataJSON); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: scan calibration waiter: %w", err)
		}
		var metadata struct {
			Binding struct {
				Mode  string `json:"mode"`
				Alias string `json:"alias"`
			} `json:"binding"`
		}
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: decode calibration waiter %s/%s: %w", planID, taskID, err)
		}
		if metadata.Binding.Mode == "await-calibration" && metadata.Binding.Alias == alias {
			waiters = append(waiters, waiter{planID: planID, taskID: taskID})
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("store: close calibration waiters: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: iterate calibration waiters: %w", err)
	}
	for _, waiter := range waiters {
		res, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET status = 'pending', claimed_by_session = NULL, claimed_by_worker_id = NULL
			WHERE plan_id = ? AND id = ? AND status = 'blocked_capability'
		`, waiter.planID, waiter.taskID)
		if err != nil {
			return fmt.Errorf("store: readmit calibration waiter: %w", err)
		}
		count, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: calibration waiter rows affected: %w", err)
		}
		if count == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE task_metadata SET blocked_reason = NULL
			WHERE plan_id = ? AND task_id = ?
		`, waiter.planID, waiter.taskID); err != nil {
			return fmt.Errorf("store: clear calibration waiter reason: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO events(plan_id, task_id, kind, actor, stream, payload_json)
			VALUES (?, ?, 'task.requeued', 'calibration', 'task', ?)
		`, waiter.planID, waiter.taskID, payloadJSON(EventPayload{
			Reason: "calibration alias " + alias + " became available",
		})); err != nil {
			return fmt.Errorf("store: log calibration readmission: %w", err)
		}
	}
	return nil
}

// GetProviderCalibration loads one immutable calibration by content address.
func (s *Store) GetProviderCalibration(ctx context.Context, id string) (ProviderCalibration, error) {
	return s.getProviderCalibration(ctx, `id = ?`, id)
}

// GetProviderCalibrationByAlias resolves a stable alias. Aliases are unique and
// immutable, so this lookup can never drift to a replacement record.
func (s *Store) GetProviderCalibrationByAlias(
	ctx context.Context,
	alias string,
) (ProviderCalibration, error) {
	return s.getProviderCalibration(ctx, `alias = ?`, alias)
}

func (s *Store) getProviderCalibration(
	ctx context.Context,
	predicate, predicateValue string,
) (ProviderCalibration, error) {
	var calibration ProviderCalibration
	var capabilitiesJSON string
	query := `
		SELECT id, alias, provider, model, effort, binary_path, binary_version,
		       binary_sha256, invocation_hash, inference_domain, control_domain,
		       independence_domain, COALESCE(model_digest,''),
		       capabilities_json, evidence_json
		FROM provider_calibrations WHERE ` + predicate
	err := s.db.QueryRowContext(ctx, query, predicateValue).Scan(
		&calibration.ID, &calibration.Alias, &calibration.Provider, &calibration.Model, &calibration.Effort,
		&calibration.BinaryPath, &calibration.BinaryVersion, &calibration.BinarySHA256,
		&calibration.InvocationHash, &calibration.InferenceDomain, &calibration.ControlDomain,
		&calibration.IndependenceDomain, &calibration.ModelDigest,
		&capabilitiesJSON, &calibration.EvidenceJSON,
	)
	if err != nil {
		return ProviderCalibration{}, fmt.Errorf("store: get provider calibration: %w", err)
	}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &calibration.Capabilities); err != nil {
		return ProviderCalibration{}, fmt.Errorf("store: decode calibration capabilities: %w", err)
	}
	return calibration, nil
}

// ListProviderCalibrations returns immutable binding records in alias order.
func (s *Store) ListProviderCalibrations(ctx context.Context) ([]ProviderCalibration, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM provider_calibrations ORDER BY alias`)
	if err != nil {
		return nil, fmt.Errorf("store: list provider calibrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan provider calibration id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate provider calibrations: %w", err)
	}
	values := make([]ProviderCalibration, 0, len(ids))
	for _, id := range ids {
		value, err := s.GetProviderCalibration(ctx, id)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}
