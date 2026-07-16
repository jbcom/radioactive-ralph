package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Fingerprint identity kinds. A project is identified by the SET of these
// rows in project_identifiers, not by a single fragile path — see §5b of
// the supervisor-architecture design.
const (
	FingerprintKindAbsPath       = "abs_path"
	FingerprintKindGitRootCommit = "git_root_commit"
	FingerprintKindGitRemote     = "git_remote"
)

// Fingerprint is one accumulated identity signal for a project.
type Fingerprint struct {
	Kind  string
	Value string
}

// ResolveProject looks up an existing project by matching ANY of the given
// fingerprints against project_identifiers. Returns found=false (no error)
// when no fingerprint matches.
func (s *Store) ResolveProject(ctx context.Context, fps []Fingerprint) (projectID string, found bool, err error) {
	for _, fp := range fps {
		if fp.Kind == "" || fp.Value == "" {
			continue
		}
		var id string
		err := s.db.QueryRowContext(ctx, `
			SELECT project_id FROM project_identifiers WHERE kind = ? AND value = ?
		`, fp.Kind, fp.Value).Scan(&id)
		if err == nil {
			return id, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", false, fmt.Errorf("store: resolve project: %w", err)
		}
	}
	return "", false, nil
}

// CreateProject inserts a new project row and seeds it with the given
// fingerprints. Returns the newly generated UUID v7 id.
func (s *Store) CreateProject(ctx context.Context, displayName string, fps []Fingerprint) (string, error) {
	id := s.uuid()
	now := s.clock.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO projects(id, display_name, created_at, updated_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
	`, id, nullIfEmpty(displayName), now, now, now)
	if err != nil {
		return "", fmt.Errorf("store: insert project: %w", err)
	}

	if err := insertIdentifiers(ctx, tx, id, fps); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("store: commit: %w", err)
	}
	return id, nil
}

// AddProjectIdentifiers accumulates additional fingerprints onto an
// existing project. Idempotent: fingerprints already present (for this or
// any other project) are silently skipped via INSERT OR IGNORE, since
// (kind, value) is the primary key of project_identifiers.
func (s *Store) AddProjectIdentifiers(ctx context.Context, projectID string, fps []Fingerprint) error {
	if projectID == "" {
		return fmt.Errorf("store: projectID required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := insertIdentifiers(ctx, tx, projectID, fps); err != nil {
		return err
	}
	return tx.Commit()
}

// insertIdentifiers writes each fingerprint via INSERT OR IGNORE so
// accumulation is idempotent — a fingerprint already recorded (for this
// project, from an earlier run) is a no-op rather than an error.
func insertIdentifiers(ctx context.Context, tx *sql.Tx, projectID string, fps []Fingerprint) error {
	for _, fp := range fps {
		if fp.Kind == "" || fp.Value == "" {
			continue
		}
		_, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO project_identifiers(project_id, kind, value)
			VALUES (?, ?, ?)
		`, projectID, fp.Kind, fp.Value)
		if err != nil {
			return fmt.Errorf("store: insert identifier %s=%s: %w", fp.Kind, fp.Value, err)
		}
	}
	return nil
}

// TouchProjectLastSeen updates last_seen_at to now. Called whenever a
// project is resolved/used so operators can see recency.
func (s *Store) TouchProjectLastSeen(ctx context.Context, projectID string) error {
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE projects SET last_seen_at = ? WHERE id = ?`, now, projectID)
	if err != nil {
		return fmt.Errorf("store: touch project: %w", err)
	}
	return nil
}

// Fingerprints computes the identity fingerprints for a directory: always
// the cleaned absolute path, plus — best-effort, if dir is a git
// repository — the root-commit sha and any "origin" remote URL. A
// fingerprint whose git command fails (e.g. no origin remote configured)
// is silently skipped rather than failing the whole call.
func Fingerprints(ctx context.Context, dir string) ([]Fingerprint, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("store: abs path: %w", err)
	}
	abs = filepath.Clean(abs)

	fps := []Fingerprint{{Kind: FingerprintKindAbsPath, Value: abs}}

	if !isGitRepo(ctx, abs) {
		return fps, nil
	}

	if sha, err := runGit(ctx, abs, "rev-list", "--max-parents=0", "HEAD"); err == nil {
		if sha = strings.TrimSpace(sha); sha != "" {
			// A repo can have multiple root commits (unlikely, but
			// rev-list can print more than one). Use the first line only.
			if nl := strings.IndexByte(sha, '\n'); nl >= 0 {
				sha = sha[:nl]
			}
			fps = append(fps, Fingerprint{Kind: FingerprintKindGitRootCommit, Value: sha})
		}
	}

	if remote, err := runGit(ctx, abs, "remote", "get-url", "origin"); err == nil {
		if remote = strings.TrimSpace(remote); remote != "" {
			fps = append(fps, Fingerprint{Kind: FingerprintKindGitRemote, Value: remote})
		}
	}

	return fps, nil
}

// isGitRepo reports whether dir is inside a git working tree, via
// `git rev-parse --is-inside-work-tree` rather than a bare .git stat so
// worktrees and submodules are recognized correctly.
func isGitRepo(ctx context.Context, dir string) bool {
	out, err := runGit(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// runGit runs `git <args...>` with cwd=dir and returns stdout. args are
// always literal flags/subcommands supplied by this file, never
// user-supplied strings, so the "subprocess launched with variable" gosec
// finding is a false positive here — only dir (the working directory) is
// caller-controlled.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	//nolint:gosec // G204: args are hardcoded git subcommands/flags from this file, not user input
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
