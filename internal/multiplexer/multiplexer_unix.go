//go:build unix

package multiplexer

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// SpawnDetached spawns req fully detached using the Detacher's chosen
// backend. See the package-level comment for the precedence order.
func (d *Detacher) SpawnDetached(req SpawnRequest) (Spawned, error) {
	if req.Name == "" {
		return Spawned{}, errors.New("multiplexer: SpawnRequest.Name required")
	}
	if req.SessionName == "" {
		return Spawned{}, errors.New("multiplexer: SpawnRequest.SessionName required")
	}
	if req.LogPath == "" {
		return Spawned{}, errors.New("multiplexer: SpawnRequest.LogPath required")
	}

	switch d.backend {
	case BackendTmux:
		return d.spawnTmux(req)
	case BackendScreen:
		return d.spawnScreen(req)
	case BackendSetsid:
		return d.spawnSetsid(req)
	case BackendUnknown:
		return Spawned{}, ErrNoBackend
	}
	return Spawned{}, ErrNoBackend
}

// spawnTmux runs `tmux new-session -d -s <name>` with the target command.
// tmux reads its own config, so there's nothing we need to do about
// stdin/stdout — tmux captures them into the session. We additionally
// set `pipe-pane` to tee output to the log file so `ralph attach`-via-log
// works without having to re-attach to the tmux session.
func (d *Detacher) spawnTmux(req SpawnRequest) (Spawned, error) {
	// Build: tmux new-session -d -s <session> <cmd> <args...>
	args := make([]string, 0, 5+len(req.Args))
	args = append(args, "new-session", "-d", "-s", req.SessionName, req.Name)
	args = append(args, req.Args...)

	cmd := exec.Command("tmux", args...) //nolint:gosec // tmux path + args are caller-controlled
	cmd.Env = req.Env
	cmd.Dir = req.Dir
	if err := cmd.Run(); err != nil {
		return Spawned{}, fmt.Errorf("multiplexer: tmux new-session: %w", err)
	}

	// pipe-pane the output to our log for `ralph attach` fallback reads.
	// Non-fatal if it fails — the supervisor can still run.
	pipeCmd := exec.Command( //nolint:gosec // values are controlled
		"tmux", "pipe-pane", "-t", req.SessionName,
		fmt.Sprintf("cat >> %s", req.LogPath),
	)
	_ = pipeCmd.Run()

	return Spawned{Descriptor: req.SessionName}, nil
}

// spawnScreen runs `screen -dmS <name> <cmd> <args...>`. Screen's `-L`
// flag enables logging but the log path is derived from session name;
// we can override via $SCREENLOG but it's simpler to post-hoc `screen -X`
// logfile settings.
func (d *Detacher) spawnScreen(req SpawnRequest) (Spawned, error) {
	args := make([]string, 0, 4+len(req.Args))
	args = append(args, "-L", "-dmS", req.SessionName, req.Name)
	args = append(args, req.Args...)

	cmd := exec.Command("screen", args...) //nolint:gosec // values are controlled
	cmd.Env = req.Env
	cmd.Dir = req.Dir
	if err := cmd.Run(); err != nil {
		return Spawned{}, fmt.Errorf("multiplexer: screen -dmS: %w", err)
	}
	return Spawned{Descriptor: req.SessionName}, nil
}

// spawnSetsid does a single fork with syscall.Setsid to fully detach.
// stdin goes to /dev/null; stdout+stderr are appended to the log file.
// The returned Spawned carries the grandchild PID so the supervisor
// caller can write it to the PID lock file.
func (d *Detacher) spawnSetsid(req SpawnRequest) (Spawned, error) {
	logFile, err := os.OpenFile(req.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // path is caller-controlled
	if err != nil {
		return Spawned{}, fmt.Errorf("multiplexer: open log %s: %w", req.LogPath, err)
	}
	defer func() { _ = logFile.Close() }()

	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return Spawned{}, fmt.Errorf("multiplexer: open /dev/null: %w", err)
	}
	defer func() { _ = devNull.Close() }()

	cmd := exec.Command(req.Name, req.Args...) //nolint:gosec // values are controlled
	cmd.Env = req.Env
	cmd.Dir = req.Dir
	cmd.Stdin = devNull
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// SysProcAttr.Setsid creates a new session so the child is not
	// killed when the parent shell exits. Setpgid groups the child
	// separately so signals to the parent pgroup don't propagate.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		return Spawned{}, fmt.Errorf("multiplexer: setsid exec: %w", err)
	}

	// We do not Wait — the process runs independently. Release so the
	// Go runtime doesn't hold onto its resources. The PID is valid even
	// after the parent exits because the child was Setsid'd out.
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		return Spawned{PID: pid}, fmt.Errorf("multiplexer: release process: %w", err)
	}
	return Spawned{PID: pid}, nil
}
