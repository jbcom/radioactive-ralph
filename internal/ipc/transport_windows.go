//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func listenEndpoint(endpoint string) (net.Listener, error) {
	listener, err := winio.ListenPipe(endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("ipc: listen pipe %s: %w", endpoint, err)
	}
	return listener, nil
}

func cleanupEndpoint(_ string) error {
	return nil
}

func dialEndpoint(ctx context.Context, endpoint string, timeout time.Duration) (net.Conn, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	conn, err := winio.DialPipeContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("ipc: dial %s: %w", endpoint, err)
	}
	return conn, nil
}
