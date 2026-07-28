package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// readyPartitionOrdinal is a partition's operator-facing identity: stable for
// the same partition, distinct across partitions, and free of author content.
//
// It hashes rather than exposes (GroupPath, BindingKey) because BindingKey is
// NOT content-free -- it re-encodes the author's own binding fields, so a
// provider/model/alias/fixture string written in the plan would appear in it
// verbatim. The observe boundary withholds author-written text (descriptions,
// acceptance commands, artifacts), and a partition label must not be the hole
// in it.
//
// A hash is the right shape here because the operator needs only to tell
// partitions APART ("these three tasks go to one turn, that one doesn't") --
// never to read what pinned them. The full binding stays available to anyone
// who legitimately holds the plan.
func readyPartitionOrdinal(p ReadyPartition) string {
	// Length-prefixed, not delimiter-joined: either field can contain any byte,
	// so a separator would let ("a|b", "c") and ("a", "b|c") hash identically
	// and merge two distinct partitions into one ordinal. This is the same
	// ambiguity that made declaredBindingKey abandon "|" joining.
	sum := sha256.Sum256(fmt.Appendf(nil, "%d:%s%d:%s",
		len(p.GroupPath), p.GroupPath, len(p.BindingKey), p.BindingKey))
	return hex.EncodeToString(sum[:8])
}

// ReadyPartition is one dispatchable wave slice: the tasks that are ready RIGHT
// NOW and share a leaf group.
//
// Dispatch cannot treat "several tasks are ready" as "several tasks may be
// delegated together". Native fan-out hands a whole partition to ONE provider
// under one group heading, so the unit of fan-out is the leaf group, not the
// ready set. Two tasks from different groups being simultaneously ready is the
// normal case for a DAG and says nothing about whether one worker may own both.
type ReadyPartition struct {
	// GroupPath is the persisted leaf-group identity shared by every task in
	// Tasks (a dotted StepRef path such as "0.2"). Empty for tasks created
	// without a task_metadata row.
	GroupPath string
	// BindingKey is the task's DECLARED per-task binding, canonicalized, shared
	// by every task in Tasks. Empty means no binding was declared.
	//
	// It partitions alongside GroupPath because a partition is delegated to ONE
	// provider in ONE turn: two same-group tasks pinning different providers
	// cannot both be honoured by a single turn, so merging them would discard a
	// restriction the plan author wrote down. An UNPINNED task gets its own key
	// rather than joining a pinned partition -- an absent binding means the pool
	// resolves it, which is not the same claim as "compatible with that pin".
	BindingKey string
	Tasks      []Task
}

// ReadyPartitions returns the currently-ready tasks for planID, grouped by
// their persisted group_path and ordered deterministically.
//
// Readiness is the SAME NOT EXISTS walk over task_deps that Ready and
// ClaimNextReady use — this adds partitioning on top of it, it does not
// introduce a second notion of ready. The join to task_metadata is a LEFT join
// on purpose: a task materialized by the plain CreateTask path has no metadata
// row, and dropping it here would make its plan silently unrunnable.
//
// Partitions come back in group-path order, tasks within a partition in the
// same sequence_ordinal/created_at order ClaimNextReady picks them, so a caller
// that dispatches partition-by-partition reproduces author order.
//
// The two FAIL-CLOSED blocked states are included deliberately, which reads
// backwards until you follow what releases them. `blocked_capability` and
// `blocked_input` are cleared by the dispatch-time gates themselves
// (capabilityGateBlocks, pathGateBlocks) when the operator has fixed the
// binding or the declaration: the gate re-checks, sees the requirement met, and
// clears the block. A task the walk never surfaces never reaches its gate, so
// excluding these states made the block outlive its own remedy — the exact
// permanent stall the gates exist to prevent. Surfacing them costs one gate
// evaluation per tick and nothing else: a still-blocked task is re-blocked by
// the same gate before any worker, session, or dispatch slot is allocated.
//
// ready_pending_approval is NOT included, and the asymmetry is the point. An
// approval gate is released by a HUMAN decision recorded out of band, never by
// re-running the gate, so surfacing it would return a task that dispatch must
// unconditionally re-block on every tick with nothing gained.
func (s *Store) ReadyPartitions(ctx context.Context, planID string) ([]ReadyPartition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.plan_id, t.description, t.status, t.parallel_group,
		       t.sequence_ordinal, COALESCE(t.acceptance_json,''),
		       COALESCE(t.claimed_by_session,''), COALESCE(t.claimed_by_worker_id,''),
		       t.retry_count, t.reclaim_count, COALESCE(t.parent_task_id,''),
		       t.created_at, t.updated_at,
		       COALESCE(m.group_path,''), COALESCE(m.metadata_json,'')
		FROM tasks t
		LEFT JOIN task_metadata m
		       ON m.plan_id = t.plan_id AND m.task_id = t.id
		WHERE t.plan_id = ?
		  AND t.status IN ('pending', 'ready',
		                   'blocked_capability', 'blocked_input')
		  AND NOT EXISTS (
		    SELECT 1 FROM task_deps d
		     JOIN tasks tdep ON tdep.plan_id = d.plan_id AND tdep.id = d.depends_on
		    WHERE d.plan_id = t.plan_id
		      AND d.task_id = t.id
		      AND tdep.status NOT IN ('done', 'skipped', 'decomposed')
		  )
		ORDER BY
		  COALESCE(m.group_path,''),
		  CASE WHEN t.sequence_ordinal IS NULL THEN 1 ELSE 0 END,
		  t.sequence_ordinal,
		  t.created_at
	`, planID)
	if err != nil {
		return nil, fmt.Errorf("store: query ready partitions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var parts []ReadyPartition
	partitionIndex := map[string]int{}
	for rows.Next() {
		var (
			t            Task
			groupPath    string
			metadataJSON string
			status       string
		)
		if err := rows.Scan(
			&t.ID, &t.PlanID, &t.Description, &status, &t.ParallelGroup,
			&t.SequenceOrdinal, &t.AcceptanceJSON,
			&t.ClaimedBySession, &t.ClaimedByWorkerID,
			&t.RetryCount, &t.ReclaimCount, &t.ParentTaskID,
			&t.CreatedAt, &t.UpdatedAt,
			&groupPath, &metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("store: scan ready partition row: %w", err)
		}
		t.Status = TaskStatus(status)

		// A partition is "tasks ONE worker may own in ONE turn", so group path
		// alone is not sufficient: two tasks in the same leaf group can pin
		// different providers, and merging them silently discards one pin.
		binding := declaredBindingKey(metadataJSON)

		// Indexed by (group, binding) rather than relying on row adjacency. The
		// query orders by group_path only -- metadata_json cannot drive ordering
		// because it also carries per-task fields (id, description) that differ
		// between tasks sharing a binding, so ordering by it would split a
		// partition that should hold together.
		key := groupPath + "\x00" + binding
		idx, seen := partitionIndex[key]
		if !seen {
			idx = len(parts)
			partitionIndex[key] = idx
			parts = append(parts, ReadyPartition{GroupPath: groupPath, BindingKey: binding})
		}
		parts[idx].Tasks = append(parts[idx].Tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate ready partitions: %w", err)
	}
	return parts, nil
}

// declaredBindingKey canonicalizes a task's DECLARED per-task binding into a
// comparison key, or "" when the task declared none.
//
// Canonicalized rather than compared as raw metadata_json, because that JSON
// also carries per-task fields (id, description, after) that differ between
// tasks which nonetheless share a binding. Keying on the raw document would
// split a partition that should hold together -- fan-out lost for nothing.
//
// The store deliberately does NOT import internal/plan: metadata_json is
// authored upstream and this only needs one sub-object, so it decodes the
// minimum rather than coupling the storage layer to the plan model. An
// undecodable document yields "", which groups it with other unpinned tasks;
// that is the safe direction, since an unpinned partition is dispatched by the
// ordinary pool path rather than under someone else's pin.
func declaredBindingKey(metadataJSON string) string {
	if metadataJSON == "" {
		return ""
	}
	var doc struct {
		Binding *declaredBinding `json:"binding"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &doc); err != nil || doc.Binding == nil {
		return ""
	}
	// A binding object present but entirely empty pins nothing, so it must key
	// the same as an absent one -- otherwise `"binding":{}` would split a
	// partition while declaring no restriction at all.
	if *doc.Binding == (declaredBinding{}) {
		return ""
	}
	// Re-encoded as JSON rather than joined with a separator. A delimiter-joined
	// key is AMBIGUOUS whenever a field can contain the delimiter, and nothing
	// forbids that here: provider "claude|x" + model "y" and provider "claude" +
	// model "x|y" produced BYTE-IDENTICAL keys, so two differently-pinned tasks
	// merged into one partition -- defeating the restriction this key exists to
	// preserve. JSON escapes its own delimiters, so the encoding is injective.
	//
	// Field order is fixed by the struct definition, so encoding/json emits the
	// same bytes for the same binding on every call.
	key, err := json.Marshal(doc.Binding)
	if err != nil {
		// Unreachable for a struct of strings and an int, but a marshal error
		// must not silently become "" -- that would merge this task into the
		// unpinned partition, which is the failure this function prevents.
		return "unencodable:" + metadataJSON
	}
	return string(key)
}

// declaredBinding is the subset of a task's metadata that pins its provider
// identity. The store deliberately does NOT import internal/plan: metadata_json
// is authored upstream and only this sub-object matters here, so it decodes the
// minimum rather than coupling storage to the plan model.
type declaredBinding struct {
	Mode        string `json:"mode"`
	Alias       string `json:"alias"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	Calibration string `json:"calibration"`
	Repetitions int    `json:"repetitions"`
	Fixture     string `json:"fixture"`
}
