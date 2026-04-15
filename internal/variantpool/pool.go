// Package variantpool manages the subprocess lifecycle of spawned
// ralph variants (green, professor, fixit, etc.). Each spawned
// subprocess gets a lifeline pipe — when the supervising MCP server
// dies for any reason (clean exit, SIGKILL, OOM, crash), the child
// reads EOF on the pipe and self-terminates within a few seconds.
//
// Per-platform belt-and-suspenders (Pdeathsig / kqueue NOTE_EXIT /
// Job Objects) is deferred to internal/proclife — the lifeline pipe
// is portable and sufficient for the portable/durable-foreground
// cases.
package variantpool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/plandag"
	"github.com/jbcom/radioactive-ralph/internal/proclife"
)

// Pool owns every live subprocess the MCP server has spawned on
// behalf of this session. Callers get back a handle per spawn.
type Pool struct {
	store     *plandag.Store
	sessionID string

	mu       sync.Mutex
	variants map[string]*Variant // keyed by plandag session_variants.id
}

// Options configures a new Pool.
type Options struct {
	// Store is the plandag handle used for heartbeat + variant row
	// bookkeeping. Required.
	Store *plandag.Store

	// SessionID is the plandag sessions.id this pool owns. Every
	// spawned variant is registered under this session.
	SessionID string
}

// New allocates a Pool. Callers must call Close() on shutdown so
// subprocess children receive SIGTERM and DB rows clean up.
func New(o Options) (*Pool, error) {
	if o.Store == nil {
		return nil, errors.New("variantpool: Store required")
	}
	if o.SessionID == "" {
		return nil, errors.New("variantpool: SessionID required")
	}
	return &Pool{
		store:     o.Store,
		sessionID: o.SessionID,
		variants:  make(map[string]*Variant),
	}, nil
}

// Get returns the variant managed under id, or nil if the pool
// doesn't know about it (e.g., it was killed or never spawned).
// Read-only — callers should treat the return value as a snapshot
// handle.
func (p *Pool) Get(id string) *Variant {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.variants[id]
}

// Close SIGTERMs every managed variant and waits up to graceShutdown
// for each to exit, SIGKILLing stragglers.
func (p *Pool) Close(ctx context.Context) error {
	p.mu.Lock()
	all := make([]*Variant, 0, len(p.variants))
	for _, v := range p.variants {
		all = append(all, v)
	}
	p.mu.Unlock()

	var firstErr error
	for _, v := range all {
		if err := v.Kill(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SpawnOpts configures a Spawn call.
type SpawnOpts struct {
	// VariantName names the variant (green, professor, ...).
	VariantName string

	// ClaudeBin is the absolute path to the claude binary that the
	// variant will exec. The variantpool itself does NOT spawn
	// claude directly — it spawns a ralph _supervisor command that
	// owns the claude lifecycle. Empty defaults to looking up
	// "claude" on PATH inside the supervisor.
	ClaudeBin string

	// RalphBin is the absolute path to the radioactive_ralph binary.
	// Pool exec's it as "<ralph> _supervisor --variant <name>".
	RalphBin string

	// WorkingDir is the working directory for the subprocess.
	WorkingDir string

	// Env is merged with the parent environment. Pool adds
	// RALPH_LIFELINE_FD=3 automatically.
	Env []string

	// LogSink receives the subprocess stdout+stderr. Nil → os.Stderr.
	LogSink io.Writer
}

// Variant is a managed subprocess handle.
type Variant struct {
	// ID is the plandag session_variants.id.
	ID string

	// Name is the variant name (green, professor, ...).
	Name string

	pool     *Pool
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	lifeline *os.File // parent-side write half; close to signal child

	done chan struct{}
}

// Spawn launches a variant subprocess with a lifeline pipe attached
// on FD 3. Returns a Variant handle; the caller can Say/Kill/Status.
func (p *Pool) Spawn(ctx context.Context, o SpawnOpts) (*Variant, error) {
	if o.VariantName == "" || o.RalphBin == "" {
		return nil, errors.New("variantpool: VariantName and RalphBin required")
	}

	// Lifeline pipe: parent holds write, child reads. Closing write
	// end (in this process) causes child's read end to return EOF.
	// Child's watchdog goroutine sees the EOF and self-terminates.
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("variantpool: pipe: %w", err)
	}

	args := []string{"_supervisor", "--variant", o.VariantName, "--foreground"}
	cmd := exec.CommandContext(ctx, o.RalphBin, args...) //nolint:gosec // ralph binary path is ours
	cmd.Dir = o.WorkingDir
	cmd.Env = append(os.Environ(), o.Env...)
	cmd.Env = append(cmd.Env, "RALPH_LIFELINE_FD=3")

	// FD 3 in the child = read end of the lifeline pipe. FDs 0/1/2
	// are the usual stdin/stdout/stderr.
	cmd.ExtraFiles = []*os.File{readEnd}

	// Platform-specific parent-death + process-group setup. On Linux
	// this sets Pdeathsig; on macOS Setpgid; on Windows arms the
	// Job Object (bound post-start below). Complements the lifeline
	// pipe with belt-and-suspenders.
	if err := proclife.Attach(cmd); err != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return nil, fmt.Errorf("variantpool: proclife.Attach: %w", err)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return nil, fmt.Errorf("variantpool: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return nil, fmt.Errorf("variantpool: stdout: %w", err)
	}

	sink := o.LogSink
	if sink == nil {
		sink = os.Stderr
	}
	cmd.Stderr = sink

	if err := cmd.Start(); err != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return nil, fmt.Errorf("variantpool: start: %w", err)
	}
	// Child has its own copy of readEnd now; we can close our copy.
	_ = readEnd.Close()

	// Windows binds the Job Object after Start (needed to kill the
	// child when we exit); POSIX variants are no-ops.
	if err := proclife.PostStart(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = writeEnd.Close()
		return nil, fmt.Errorf("variantpool: proclife.PostStart: %w", err)
	}

	startTime := time.Now().UTC().Format(time.RFC3339Nano)
	svID, err := p.store.CreateSessionVariant(ctx, plandag.SessionVariantOpts{
		SessionID:           p.sessionID,
		VariantName:         o.VariantName,
		SubprocessPID:       cmd.Process.Pid,
		SubprocessStartTime: startTime,
	})
	if err != nil {
		_ = cmd.Process.Kill()
		_ = writeEnd.Close()
		return nil, fmt.Errorf("variantpool: register session_variant: %w", err)
	}

	v := &Variant{
		ID:       svID,
		Name:     o.VariantName,
		pool:     p,
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		lifeline: writeEnd,
		done:     make(chan struct{}),
	}

	go func() {
		_ = cmd.Wait()
		close(v.done)
	}()

	p.mu.Lock()
	p.variants[svID] = v
	p.mu.Unlock()

	return v, nil
}

// Say writes a line to the subprocess's stdin. The trailing \n is
// added if not present.
func (v *Variant) Say(msg string) error {
	if v.stdin == nil {
		return errors.New("variantpool: variant not running")
	}
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg += "\n"
	}
	_, err := io.WriteString(v.stdin, msg)
	return err
}

// ReadOne reads the next line of stdout. Callers typically loop
// calling this in a goroutine to drain output.
func (v *Variant) ReadOne(buf []byte) (int, error) {
	return v.stdout.Read(buf)
}

// Kill signals the subprocess to shut down. Closes the lifeline
// pipe (which alone causes a well-behaved child to exit), then
// SIGTERMs, then SIGKILLs after graceWait.
func (v *Variant) Kill(ctx context.Context) error {
	const graceWait = 3 * time.Second

	// 1. Close lifeline — polite shutdown for children that watch it.
	if v.lifeline != nil {
		_ = v.lifeline.Close()
		v.lifeline = nil
	}

	// 2. SIGTERM.
	if v.cmd.Process != nil {
		_ = v.cmd.Process.Signal(syscall.SIGTERM)
	}

	// 3. Wait up to graceWait for voluntary exit.
	select {
	case <-v.done:
		return v.finalize(ctx)
	case <-time.After(graceWait):
		// Still alive — SIGKILL.
		if v.cmd.Process != nil {
			_ = v.cmd.Process.Kill()
		}
		<-v.done
		return v.finalize(ctx)
	case <-ctx.Done():
		// Caller canceled; still SIGKILL on the way out so we don't
		// leak.
		if v.cmd.Process != nil {
			_ = v.cmd.Process.Kill()
		}
		return ctx.Err()
	}
}

// finalize marks the session_variant row terminated and removes the
// variant from the pool map.
func (v *Variant) finalize(ctx context.Context) error {
	v.pool.mu.Lock()
	delete(v.pool.variants, v.ID)
	v.pool.mu.Unlock()

	// Update the DB row. Failures here are logged but not propagated —
	// the subprocess is already gone, the reaper will clean up on
	// next sweep anyway.
	_, err := v.pool.store.DB().ExecContext(ctx, `
		UPDATE session_variants SET status = 'terminated'
		WHERE id = ?
	`, v.ID)
	return err
}

// Status returns a snapshot of the subprocess state.
type Status struct {
	ID      string
	Name    string
	PID     int
	Running bool
}

// Status produces a lightweight read-only snapshot suitable for
// MCP tool responses.
func (v *Variant) Status() Status {
	running := true
	select {
	case <-v.done:
		running = false
	default:
	}
	pid := 0
	if v.cmd.Process != nil {
		pid = v.cmd.Process.Pid
	}
	return Status{
		ID:      v.ID,
		Name:    v.Name,
		PID:     pid,
		Running: running,
	}
}
