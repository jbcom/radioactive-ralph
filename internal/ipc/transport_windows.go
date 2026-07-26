//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"sync"
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

// go-winio v0.6.2 can block forever in win32PipeListener.Close while an
// overlapped ConnectNamedPipe is pending (microsoft/go-winio#357). Wake the
// server's outstanding Accept first, wait for our accept loop to leave, and
// still bound both the dependency Close and Ralph's own goroutine drain. A
// pathological kernel/dependency state may leak the stuck dependency goroutine
// until process exit, but it can never make supervisor shutdown unbounded.
func closeEndpointListener(listener net.Listener, endpoint string, acceptDone <-chan struct{}) error {
	select {
	case <-acceptDone:
		// The accept loop observed stop before entering another Accept.
	default:
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		conn, err := dialEndpoint(ctx, endpoint, time.Second)
		cancel()
		if err == nil {
			_ = conn.Close()
		}
		select {
		case <-acceptDone:
		case <-time.After(2 * time.Second):
			return fmt.Errorf("ipc: timed out waking Windows named-pipe accept loop")
		}
	}

	closed := make(chan error, 1)
	go func() {
		closed <- listener.Close()
	}()
	select {
	case err := <-closed:
		return err
	case <-time.After(2 * time.Second):
		return fmt.Errorf("ipc: timed out closing Windows named-pipe listener")
	}
}

func waitForServerDrain(wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("ipc: timed out draining Windows server goroutines")
	}
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
