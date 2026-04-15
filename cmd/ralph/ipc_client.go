package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/variant"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
)

// socketPath returns the Unix socket + heartbeat file for a variant in
// a given repo.
func socketPath(repoRoot string, v variant.Name) (socket, heartbeat string, err error) {
	paths, err := xdg.Resolve(repoRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve paths: %w", err)
	}
	socket = filepath.Join(paths.Sessions, string(v)+".sock")
	heartbeat = socket + ".alive"
	return socket, heartbeat, nil
}

// ensureAlive reports an actionable error if the supervisor doesn't
// appear to be running. Uses the heartbeat file mtime to distinguish
// "supervisor alive" from "stale socket from a crashed supervisor".
func ensureAlive(socket, heartbeat string) error {
	if _, err := os.Stat(socket); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no supervisor socket at %s (is ralph running?)", socket)
	}
	if !ipc.SocketAlive(heartbeat, 2*time.Minute) {
		return fmt.Errorf(
			"supervisor socket exists but heartbeat is stale (>2m); the supervisor likely crashed — remove %s and %s and rerun `ralph run`",
			socket, heartbeat,
		)
	}
	return nil
}

// roundTrip connects to the supervisor socket, sends req, and returns
// the first Response.
func roundTrip(ctx context.Context, socket string, req ipc.Request) (ipc.Response, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return ipc.Response{}, fmt.Errorf("dial %s: %w", socket, err)
	}
	defer func() { _ = conn.Close() }()

	data, err := json.Marshal(req)
	if err != nil {
		return ipc.Response{}, err
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return ipc.Response{}, fmt.Errorf("write: %w", err)
	}
	dec := json.NewDecoder(conn)
	var resp ipc.Response
	if err := dec.Decode(&resp); err != nil {
		return ipc.Response{}, fmt.Errorf("decode: %w", err)
	}
	return resp, nil
}

// resolveRepoRoot returns the operator's repo — defaulting to cwd.
func resolveRepoRoot(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return os.Getwd()
}
