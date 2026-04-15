package supervisor

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/variant"
	"github.com/jbcom/radioactive-ralph/internal/workspace"
)

// newTestRepo makes a throwaway git repo for Workspace to operate on.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q", "-b", "main")
	mustRun(t, dir, "git", "config", "user.email", "ralph@example.com")
	mustRun(t, dir, "git", "config", "user.name", "Ralph")
	mustRun(t, dir, "git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustRun(t, dir, "git", "add", "README.md")
	mustRun(t, dir, "git", "commit", "-q", "-m", "init")
	return dir
}

func mustRun(t *testing.T, cwd, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// shortTempDir returns a short-path tmp dir to avoid the macOS
// 104-byte Unix socket path limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "sup-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func newTestSupervisor(t *testing.T, name variant.Name) *Supervisor {
	t.Helper()
	t.Setenv("RALPH_STATE_DIR", shortTempDir(t))
	repo := newTestRepo(t)
	p, err := variant.Lookup(string(name))
	if err != nil {
		t.Fatalf("Lookup(%s): %v", name, err)
	}
	// Use shared isolation so we don't have to wait for a mirror clone
	// in every test. Blue is the only built-in with shared as its
	// natural default.
	ws, err := workspace.New(repo, p, variant.IsolationShared,
		variant.ObjectStoreReference, variant.SyncSourceBoth, variant.LFSOnDemand)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	sup, err := New(Options{
		RepoPath:          repo,
		Variant:           p,
		Workspace:         ws,
		HeartbeatInterval: 50 * time.Millisecond,
		ShutdownTimeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return sup
}

// runInBackground starts Run in a goroutine, returning a function the
// test calls to shut it down and wait for the return value.
func runInBackground(t *testing.T, sup *Supervisor) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- sup.Run(ctx) }()

	// Wait for the socket to be live.
	socketPath := filepath.Join(sup.paths.Sessions,
		string(sup.opts.Variant.Name)+".sock")
	waitForSocket(t, socketPath)

	return func() error {
		sup.Shutdown()
		cancel()
		select {
		case err := <-errCh:
			return err
		case <-time.After(10 * time.Second):
			t.Fatalf("supervisor did not shut down within 10s")
			return nil
		}
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket never appeared: %s", path)
}

// sendCommand sends a request to the supervisor's socket and returns
// the first Response line.
func sendCommand(t *testing.T, socketPath string, req ipc.Request) ipc.Response {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial %s: %v", socketPath, err)
	}
	defer func() { _ = conn.Close() }()

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}

	dec := json.NewDecoder(conn)
	var resp ipc.Response
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

// ── Tests -------------------------------------------------------------

func TestRunAndShutdown(t *testing.T) {
	sup := newTestSupervisor(t, "blue")
	stop := runInBackground(t, sup)
	if err := stop(); err != nil {
		t.Errorf("Run returned error: %v", err)
	}
}

func TestPIDLockPreventsSecondSupervisor(t *testing.T) {
	sup1 := newTestSupervisor(t, "blue")
	stop1 := runInBackground(t, sup1)
	defer func() { _ = stop1() }()

	// Second supervisor against the same repo + variant must fail at
	// flock-acquire time.
	sup2, err := New(Options{
		RepoPath:  sup1.opts.RepoPath,
		Variant:   sup1.opts.Variant,
		Workspace: sup1.opts.Workspace,
	})
	if err != nil {
		t.Fatalf("New sup2: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runErr := sup2.Run(ctx)
	if runErr == nil {
		t.Fatal("second supervisor should have failed to start (flock)")
	}
	if !strings.Contains(runErr.Error(), "flock") && !strings.Contains(runErr.Error(), "pid lock") {
		t.Errorf("expected flock error, got %v", runErr)
	}
}

func TestStatusCommand(t *testing.T) {
	sup := newTestSupervisor(t, "blue")
	stop := runInBackground(t, sup)
	defer func() { _ = stop() }()

	socketPath := filepath.Join(sup.paths.Sessions, "blue.sock")
	resp := sendCommand(t, socketPath, ipc.Request{Cmd: ipc.CmdStatus})
	if !resp.Ok {
		t.Fatalf("status Ok=false: %s", resp.Error)
	}
	var status ipc.StatusReply
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if status.Variant != "blue" {
		t.Errorf("Variant = %q, want blue", status.Variant)
	}
	if status.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", status.PID, os.Getpid())
	}
	if status.Uptime <= 0 {
		t.Errorf("Uptime = %v, want > 0", status.Uptime)
	}
}

func TestEnqueueCommand(t *testing.T) {
	sup := newTestSupervisor(t, "blue")
	stop := runInBackground(t, sup)
	defer func() { _ = stop() }()

	socketPath := filepath.Join(sup.paths.Sessions, "blue.sock")
	args, _ := json.Marshal(ipc.EnqueueArgs{
		Description: "test task",
		Priority:    5,
	})
	resp := sendCommand(t, socketPath, ipc.Request{Cmd: ipc.CmdEnqueue, Args: args})
	if !resp.Ok {
		t.Fatalf("enqueue Ok=false: %s", resp.Error)
	}
	var reply ipc.EnqueueReply
	if err := json.Unmarshal(resp.Data, &reply); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if reply.TaskID == "" {
		t.Error("expected generated TaskID")
	}
	if !reply.Inserted {
		t.Error("first enqueue should be Inserted=true")
	}
}

func TestStopCommandTriggersShutdown(t *testing.T) {
	sup := newTestSupervisor(t, "blue")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- sup.Run(ctx) }()

	socketPath := filepath.Join(sup.paths.Sessions, "blue.sock")
	waitForSocket(t, socketPath)

	args, _ := json.Marshal(ipc.StopArgs{Graceful: true})
	resp := sendCommand(t, socketPath, ipc.Request{Cmd: ipc.CmdStop, Args: args})
	if !resp.Ok {
		t.Fatalf("stop Ok=false: %s", resp.Error)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned error after stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not stop within 5s of stop command")
	}
}

func TestNewValidatesOptions(t *testing.T) {
	cases := map[string]Options{
		"no repo":      {Variant: mustProfile(t, "blue")},
		"no variant":   {RepoPath: "/tmp", Workspace: &workspace.Manager{}},
		"no workspace": {RepoPath: "/tmp", Variant: mustProfile(t, "blue")},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(opts); err == nil {
				t.Errorf("New should have failed for %q", name)
			}
		})
	}
}

func mustProfile(t *testing.T, name string) variant.Profile {
	t.Helper()
	p, err := variant.Lookup(name)
	if err != nil {
		t.Fatalf("Lookup(%s): %v", name, err)
	}
	return p
}
