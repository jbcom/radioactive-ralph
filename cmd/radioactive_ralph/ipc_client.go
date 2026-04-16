package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
)

// socketPath returns the local control-plane endpoint + heartbeat file for the
// repo service.
func socketPath(repoRoot string) (socket, heartbeat string, err error) {
	paths, err := xdg.Resolve(repoRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve paths: %w", err)
	}
	socket, heartbeat = ipc.ServiceEndpoint(paths.Sessions)
	return socket, heartbeat, nil
}

// ensureAlive reports an actionable error if the repo service doesn't
// appear to be running. Uses the heartbeat file mtime to distinguish
// "service alive" from "stale socket from a crashed runtime".
func ensureAlive(socket, heartbeat string) error {
	if _, err := os.Stat(socket); errors.Is(err, os.ErrNotExist) {
		if strings.HasPrefix(socket, `\\.\pipe\`) {
			if !ipc.SocketAlive(heartbeat, 2*time.Minute) {
				return fmt.Errorf("no repo service heartbeat at %s (is `radioactive_ralph service start` running?)", heartbeat)
			}
			return nil
		}
		return fmt.Errorf("no repo service endpoint at %s (is `radioactive_ralph service start` running?)", socket)
	}
	if !ipc.SocketAlive(heartbeat, 2*time.Minute) {
		return fmt.Errorf(
			"repo service endpoint exists but heartbeat is stale (>2m); the runtime likely crashed — remove %s and %s and rerun `radioactive_ralph service start`",
			socket, heartbeat,
		)
	}
	return nil
}

// resolveRepoRoot returns the operator's repo — defaulting to cwd.
func resolveRepoRoot(override string) (string, error) {
	repo := override
	if repo == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		repo = cwd
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", fmt.Errorf("resolve repo root %q: %w", repo, err)
	}
	if evaluated, err := filepath.EvalSymlinks(abs); err == nil {
		abs = evaluated
	}
	return abs, nil
}
