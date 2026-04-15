package variantpool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/plandag"
)

// TestSpawnAndKill walks the variantpool lifecycle against a tiny
// test binary that reads its lifeline pipe and self-terminates on
// EOF. Verifies:
//   - Spawn registers a session_variants row
//   - Kill closes the lifeline, SIGTERMs, and marks the row terminated
func TestSpawnAndKill(t *testing.T) {
	ctx := context.Background()

	// Build a throwaway "fake ralph" binary that our Spawn can exec.
	// The binary just reads lifeline FD 3 in a goroutine + sleeps;
	// on lifeline EOF it exits 0.
	ralphBin := buildFakeSupervisor(t)

	store := openTempStore(t, ctx)
	defer store.Close()

	sessID, err := store.CreateSession(ctx, plandag.SessionOpts{
		Mode:         plandag.SessionModePortable,
		Transport:    plandag.SessionTransportStdio,
		PID:          os.Getpid(),
		PIDStartTime: "test",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	pool, err := New(Options{Store: store, SessionID: sessID})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	v, err := pool.Spawn(ctx, SpawnOpts{
		VariantName: "green",
		RalphBin:    ralphBin,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	status := v.Status()
	if !status.Running {
		t.Error("Status.Running = false immediately after Spawn")
	}
	if status.PID <= 0 {
		t.Errorf("Status.PID = %d, want > 0", status.PID)
	}

	// Row should be present with status='running'.
	var rowStatus string
	err = store.DB().QueryRowContext(ctx,
		`SELECT status FROM session_variants WHERE id = ?`, v.ID).Scan(&rowStatus)
	if err != nil {
		t.Fatalf("query session_variant: %v", err)
	}
	if rowStatus != "running" {
		t.Errorf("row status = %q, want running", rowStatus)
	}

	// Kill politely; should terminate within graceWait.
	killCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := v.Kill(killCtx); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	// Row should now be terminated.
	err = store.DB().QueryRowContext(ctx,
		`SELECT status FROM session_variants WHERE id = ?`, v.ID).Scan(&rowStatus)
	if err != nil {
		t.Fatalf("requery session_variant: %v", err)
	}
	if rowStatus != "terminated" {
		t.Errorf("post-kill row status = %q, want terminated", rowStatus)
	}

	if v.Status().Running {
		t.Error("Status.Running = true after Kill")
	}
}

// TestPoolCloseKillsAll verifies Pool.Close cleans up every live
// variant at once.
func TestPoolCloseKillsAll(t *testing.T) {
	ctx := context.Background()
	ralphBin := buildFakeSupervisor(t)

	store := openTempStore(t, ctx)
	defer store.Close()

	sessID, err := store.CreateSession(ctx, plandag.SessionOpts{
		Mode:      plandag.SessionModePortable,
		Transport: plandag.SessionTransportStdio,
		PID:       os.Getpid(),
		PIDStartTime: "test",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	pool, err := New(Options{Store: store, SessionID: sessID})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := pool.Spawn(ctx, SpawnOpts{
			VariantName: "green",
			RalphBin:    ralphBin,
		}); err != nil {
			t.Fatalf("Spawn %d: %v", i, err)
		}
	}

	// All three should be running in the DB.
	var running int
	err = store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_variants WHERE session_id = ? AND status = 'running'`,
		sessID).Scan(&running)
	if err != nil {
		t.Fatalf("count running: %v", err)
	}
	if running != 3 {
		t.Errorf("running variants before Close = %d, want 3", running)
	}

	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Close(closeCtx); err != nil {
		t.Fatalf("Pool.Close: %v", err)
	}

	// All should be terminated.
	err = store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_variants WHERE session_id = ? AND status = 'terminated'`,
		sessID).Scan(&running)
	if err != nil {
		t.Fatalf("count terminated: %v", err)
	}
	if running != 3 {
		t.Errorf("terminated variants after Close = %d, want 3", running)
	}
}

// openTempStore is a shared test helper that opens a plandag Store
// against a tmpdir SQLite file.
func openTempStore(t *testing.T, ctx context.Context) *plandag.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "plans.db") +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	s, err := plandag.Open(ctx, plandag.Options{DSN: dsn})
	if err != nil {
		t.Fatalf("plandag.Open: %v", err)
	}
	return s
}

// buildFakeSupervisor compiles a tiny Go program that mimics the
// shape of `radioactive_ralph _supervisor --variant <name> --foreground`:
//   - reads stdin line-by-line forever (as a real supervisor would
//     consume operator instructions)
//   - watches FD 3 (lifeline) in a goroutine and exits 0 on EOF
//
// The test uses this instead of the real ralph binary so the test
// tree stays self-contained.
func buildFakeSupervisor(t *testing.T) string {
	t.Helper()

	src := `package main

import (
	"bufio"
	"os"
	"strconv"
)

func main() {
	// Watch lifeline pipe. EOF = parent died.
	fdStr := os.Getenv("RALPH_LIFELINE_FD")
	if fdStr != "" {
		fd, err := strconv.Atoi(fdStr)
		if err == nil {
			f := os.NewFile(uintptr(fd), "lifeline")
			go func() {
				buf := make([]byte, 1)
				for {
					_, err := f.Read(buf)
					if err != nil {
						// EOF or broken pipe — parent is gone.
						os.Exit(0)
					}
				}
			}()
		}
	}

	// Drain stdin so our parent's Say() calls don't block.
	// Exit immediately on SIGTERM (the default exit-on-signal).
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		// Echo to stdout for visibility.
		os.Stdout.Write(append(scanner.Bytes(), '\n'))
	}
}
`
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "fake_supervisor.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write fake supervisor src: %v", err)
	}
	binPath := filepath.Join(dir, "fake_supervisor")

	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake supervisor: %v\n%s", err, out)
	}
	return binPath
}
