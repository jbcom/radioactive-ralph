//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func listenEndpoint(endpoint string) (net.Listener, error) {
	cfg, err := namedPipeConfig()
	if err != nil {
		return nil, fmt.Errorf("ipc: configure pipe %s: %w", endpoint, err)
	}
	listener, err := winio.ListenPipe(endpoint, cfg)
	if err != nil {
		return nil, fmt.Errorf("ipc: listen pipe %s: %w", endpoint, err)
	}
	return listener, nil
}

func namedPipeConfig() (*winio.PipeConfig, error) {
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("current token user: %w", err)
	}
	return &winio.PipeConfig{
		// Restrict the pipe to the current user for normal attached/service-start
		// flows. When the runtime is hosted by Windows SCM as LocalSystem, allow
		// interactive users to connect so status/attach/stop still work.
		SecurityDescriptor: pipeSecurityDescriptorForSID(
			tokenUser.User.Sid.String(),
			tokenUser.User.Sid.IsWellKnown(windows.WinLocalSystemSid),
		),
	}, nil
}

func pipeSecurityDescriptorForSID(userSID string, localSystem bool) string {
	if localSystem {
		return "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)"
	}
	return fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;%s)", userSID)
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
