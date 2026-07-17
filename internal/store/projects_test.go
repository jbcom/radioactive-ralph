package store

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: DSN(filepath.Join(t.TempDir(), "store.db"))})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateAndResolveProjectByPath(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	fps := []Fingerprint{{Kind: FingerprintKindAbsPath, Value: "/tmp/my-project"}}
	id, err := s.CreateProject(ctx, "My Project", fps)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if id == "" {
		t.Fatal("CreateProject returned empty id")
	}

	gotID, found, err := s.ResolveProject(ctx, fps)
	if err != nil {
		t.Fatalf("ResolveProject: %v", err)
	}
	if !found {
		t.Fatal("ResolveProject: found = false, want true")
	}
	if gotID != id {
		t.Errorf("ResolveProject id = %q, want %q", gotID, id)
	}
}

func TestResolveProjectNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	_, found, err := s.ResolveProject(ctx, []Fingerprint{
		{Kind: FingerprintKindAbsPath, Value: "/nowhere"},
	})
	if err != nil {
		t.Fatalf("ResolveProject: %v", err)
	}
	if found {
		t.Error("ResolveProject: found = true, want false")
	}
}

// TestFingerprintAccumulation proves the central §5b identity guarantee: a
// path-only project (created before `git init`) gains a git identifier on
// top of the path identifier without losing its identity — resolving by
// EITHER fingerprint returns the same project id, and a lookup by the new
// git fingerprint alone (as if from a fresh process that only computed git
// fingerprints) still finds it.
func TestFingerprintAccumulation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	pathFP := Fingerprint{Kind: FingerprintKindAbsPath, Value: "/tmp/accum-project"}
	id, err := s.CreateProject(ctx, "", []Fingerprint{pathFP})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Simulate the directory later being `git init`-ed: we now also know
	// its root-commit sha and remote.
	gitFPs := []Fingerprint{
		{Kind: FingerprintKindGitRootCommit, Value: "deadbeefcafefeed"},
		{Kind: FingerprintKindGitRemote, Value: "git@github.com:example/accum-project.git"},
	}
	if err := s.AddProjectIdentifiers(ctx, id, gitFPs); err != nil {
		t.Fatalf("AddProjectIdentifiers: %v", err)
	}

	// Resolving by the ORIGINAL path fingerprint still works.
	gotID, found, err := s.ResolveProject(ctx, []Fingerprint{pathFP})
	if err != nil {
		t.Fatalf("ResolveProject(path): %v", err)
	}
	if !found || gotID != id {
		t.Fatalf("ResolveProject(path) = (%q, %v), want (%q, true)", gotID, found, id)
	}

	// Resolving by ONLY the new git fingerprint (as if the caller moved
	// the directory and can no longer produce the old abs_path) also
	// finds the SAME project — identity survived the transition.
	gotID, found, err = s.ResolveProject(ctx, []Fingerprint{gitFPs[0]})
	if err != nil {
		t.Fatalf("ResolveProject(git root commit): %v", err)
	}
	if !found || gotID != id {
		t.Fatalf("ResolveProject(git root commit) = (%q, %v), want (%q, true)", gotID, found, id)
	}

	gotID, found, err = s.ResolveProject(ctx, []Fingerprint{gitFPs[1]})
	if err != nil {
		t.Fatalf("ResolveProject(git remote): %v", err)
	}
	if !found || gotID != id {
		t.Fatalf("ResolveProject(git remote) = (%q, %v), want (%q, true)", gotID, found, id)
	}

	// All three fingerprints should now be recorded against the project.
	var count int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM project_identifiers WHERE project_id = ?", id).Scan(&count); err != nil {
		t.Fatalf("count identifiers: %v", err)
	}
	if count != 3 {
		t.Errorf("project_identifiers count = %d, want 3", count)
	}
}

// TestAddProjectIdentifiersIdempotent confirms re-adding an already-known
// fingerprint is a no-op, not an error — accumulation must be safe to call
// on every startup without duplicate-key failures.
func TestAddProjectIdentifiersIdempotent(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	fps := []Fingerprint{{Kind: FingerprintKindAbsPath, Value: "/tmp/idempotent"}}
	id, err := s.CreateProject(ctx, "", fps)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Re-add the same fingerprint several times.
	for i := 0; i < 3; i++ {
		if err := s.AddProjectIdentifiers(ctx, id, fps); err != nil {
			t.Fatalf("AddProjectIdentifiers[%d]: %v", i, err)
		}
	}

	var count int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM project_identifiers WHERE project_id = ?", id).Scan(&count); err != nil {
		t.Fatalf("count identifiers: %v", err)
	}
	if count != 1 {
		t.Errorf("project_identifiers count = %d, want 1 (idempotent)", count)
	}
}

// TestFingerprintsNonGitDir confirms a plain directory yields only the
// abs_path fingerprint.
func TestFingerprintsNonGitDir(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	fps, err := Fingerprints(ctx, dir)
	if err != nil {
		t.Fatalf("Fingerprints: %v", err)
	}
	if len(fps) != 1 {
		t.Fatalf("Fingerprints returned %d entries, want 1: %+v", len(fps), fps)
	}
	if fps[0].Kind != FingerprintKindAbsPath {
		t.Errorf("Fingerprints[0].Kind = %q, want %q", fps[0].Kind, FingerprintKindAbsPath)
	}
	want, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if fps[0].Value != filepath.Clean(want) {
		t.Errorf("Fingerprints[0].Value = %q, want %q", fps[0].Value, want)
	}
}

// TestFingerprintsGitDir confirms a git repo yields abs_path plus
// git_root_commit (and git_remote when an origin is configured).
func TestFingerprintsGitDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	ctx := context.Background()
	dir := t.TempDir()

	runOrSkip := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runOrSkip("init", "-q")
	runOrSkip("config", "user.email", "test@example.com")
	runOrSkip("config", "user.name", "Test")
	runOrSkip("commit", "--allow-empty", "-q", "-m", "root commit")
	runOrSkip("remote", "add", "origin", "git@github.com:example/repo.git")

	fps, err := Fingerprints(ctx, dir)
	if err != nil {
		t.Fatalf("Fingerprints: %v", err)
	}

	kinds := map[string]string{}
	for _, fp := range fps {
		kinds[fp.Kind] = fp.Value
	}
	if _, ok := kinds[FingerprintKindAbsPath]; !ok {
		t.Error("missing abs_path fingerprint")
	}
	if _, ok := kinds[FingerprintKindGitRootCommit]; !ok {
		t.Error("missing git_root_commit fingerprint")
	}
	if got := kinds[FingerprintKindGitRemote]; got != "git@github.com:example/repo.git" {
		t.Errorf("git_remote = %q, want %q", got, "git@github.com:example/repo.git")
	}
}

// TestFingerprintsGitDirNoRemote confirms a git repo with no configured
// remote still succeeds, best-effort skipping the git_remote fingerprint
// rather than failing the whole call.
func TestFingerprintsGitDirNoRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	ctx := context.Background()
	dir := t.TempDir()

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("commit", "--allow-empty", "-q", "-m", "root commit")
	// Deliberately no `git remote add`.

	fps, err := Fingerprints(ctx, dir)
	if err != nil {
		t.Fatalf("Fingerprints: %v", err)
	}
	for _, fp := range fps {
		if fp.Kind == FingerprintKindGitRemote {
			t.Errorf("unexpected git_remote fingerprint with no origin configured: %+v", fp)
		}
	}
}

func TestTouchProjectLastSeen(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	id, err := s.CreateProject(ctx, "touch-project", []Fingerprint{
		{Kind: FingerprintKindAbsPath, Value: "/tmp/touch-project"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	var before string
	if err := s.DB().QueryRowContext(ctx, "SELECT COALESCE(last_seen_at,'') FROM projects WHERE id = ?", id).Scan(&before); err != nil {
		t.Fatalf("read last_seen_at before touch: %v", err)
	}

	if err := s.TouchProjectLastSeen(ctx, id); err != nil {
		t.Fatalf("TouchProjectLastSeen: %v", err)
	}

	var after string
	if err := s.DB().QueryRowContext(ctx, "SELECT COALESCE(last_seen_at,'') FROM projects WHERE id = ?", id).Scan(&after); err != nil {
		t.Fatalf("read last_seen_at after touch: %v", err)
	}
	if after == "" {
		t.Error("last_seen_at empty after TouchProjectLastSeen")
	}
}

// TestTouchProjectLastSeenMissingProjectIsNotAnError confirms touching a
// nonexistent project id does not error — TouchProjectLastSeen has no
// RowsAffected check, unlike SetPlanStatus, so a zero-row UPDATE is
// currently a silent no-op rather than a reported failure. This test
// documents that behavior explicitly.
func TestTouchProjectLastSeenMissingProjectIsNotAnError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.TouchProjectLastSeen(ctx, "does-not-exist"); err != nil {
		t.Errorf("TouchProjectLastSeen on missing project: want nil, got %v", err)
	}
}

func TestAddProjectIdentifiersMissingProject(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	err := s.AddProjectIdentifiers(ctx, "does-not-exist", []Fingerprint{
		{Kind: FingerprintKindAbsPath, Value: "/tmp/whatever"},
	})
	if err == nil {
		t.Error("AddProjectIdentifiers against a missing project: want error (FK violation), got nil")
	}
}

func TestProjectAbsPathReturnsRecordedCheckout(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	id, err := s.CreateProject(ctx, "AbsPath Project", []Fingerprint{
		{Kind: FingerprintKindAbsPath, Value: "/work/repo"},
		{Kind: FingerprintKindGitRemote, Value: "git@github.com:me/repo.git"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	path, found, err := s.ProjectAbsPath(ctx, id)
	if err != nil {
		t.Fatalf("ProjectAbsPath: %v", err)
	}
	if !found {
		t.Fatal("ProjectAbsPath found=false for a project seeded with an abs_path")
	}
	if path != "/work/repo" {
		t.Errorf("abs path = %q, want %q", path, "/work/repo")
	}
}

func TestProjectAbsPathMissingIsNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	// A project seeded with only a git remote (no abs_path) reports not-found.
	id, err := s.CreateProject(ctx, "Remote Only", []Fingerprint{
		{Kind: FingerprintKindGitRemote, Value: "git@github.com:me/repo.git"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_, found, err := s.ProjectAbsPath(ctx, id)
	if err != nil {
		t.Fatalf("ProjectAbsPath: %v", err)
	}
	if found {
		t.Error("ProjectAbsPath found=true for a project with no abs_path identifier")
	}
}

// TestProjectAbsPathMostRecentWins is the regression for the audit's
// test-coverage finding: ProjectAbsPath's `ORDER BY added_at DESC LIMIT 1`
// is load-bearing (a project accumulates abs_paths as it's cloned/moved, and
// the most-recently-added one is the best guess at where the operator works
// now). A fake clock makes the ordering deterministic.
func TestProjectAbsPathMostRecentWins(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	id, err := s.CreateProject(ctx, "Moved Project", []Fingerprint{
		{Kind: FingerprintKindAbsPath, Value: "/old/location"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// A later move adds a second abs_path at a strictly-later timestamp.
	clock.Advance(1 * time.Hour)
	if err := s.AddProjectIdentifiers(ctx, id, []Fingerprint{
		{Kind: FingerprintKindAbsPath, Value: "/new/location"},
	}); err != nil {
		t.Fatalf("AddProjectIdentifiers: %v", err)
	}

	path, found, err := s.ProjectAbsPath(ctx, id)
	if err != nil || !found {
		t.Fatalf("ProjectAbsPath: found=%v err=%v", found, err)
	}
	if path != "/new/location" {
		t.Errorf("abs path = %q, want the most-recently-added /new/location", path)
	}
}

// TestProjectAbsPathMoveBackRefreshesTimestamp is the regression for
// CodeRabbit's finding: re-adding an already-known abs_path (a project moved
// A -> B -> A) must REFRESH its added_at so most-recent-wins picks A again,
// which INSERT OR IGNORE failed to do (it kept the stale original timestamp).
func TestProjectAbsPathMoveBackRefreshesTimestamp(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	id, err := s.CreateProject(ctx, "Bouncing Project", []Fingerprint{
		{Kind: FingerprintKindAbsPath, Value: "/path/A"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Move to B (later).
	clock.Advance(1 * time.Hour)
	if err := s.AddProjectIdentifiers(ctx, id, []Fingerprint{{Kind: FingerprintKindAbsPath, Value: "/path/B"}}); err != nil {
		t.Fatalf("add B: %v", err)
	}
	// Now B is most recent.
	if p, _, _ := s.ProjectAbsPath(ctx, id); p != "/path/B" {
		t.Fatalf("after move to B, abs path = %q, want /path/B", p)
	}

	// Move back to A (later still): re-adding A must refresh its timestamp.
	clock.Advance(1 * time.Hour)
	if err := s.AddProjectIdentifiers(ctx, id, []Fingerprint{{Kind: FingerprintKindAbsPath, Value: "/path/A"}}); err != nil {
		t.Fatalf("re-add A: %v", err)
	}
	if p, _, _ := s.ProjectAbsPath(ctx, id); p != "/path/A" {
		t.Errorf("after move back to A, abs path = %q, want /path/A (re-add must refresh added_at)", p)
	}
}
