//go:build !windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

func listenEndpoint(endpoint string) (net.Listener, error) {
	_ = os.Remove(endpoint)
	if err := os.MkdirAll(filepath.Dir(endpoint), 0o700); err != nil {
		return nil, fmt.Errorf("ipc: mkdir socket parent: %w", err)
	}
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(endpoint, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("ipc: chmod socket: %w", err)
	}
	return listener, nil
}

func cleanupEndpoint(endpoint string) error {
	return os.Remove(endpoint)
}

func dialEndpoint(ctx context.Context, endpoint string, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: timeout}
	return dialer.DialContext(ctx, "unix", endpoint)
}
