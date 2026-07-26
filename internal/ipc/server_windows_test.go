//go:build windows

package ipc

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestWindowsNamedPipeStopIsBoundedAfterProbe covers the go-winio v0.6.2
// shutdown failure seen in hosted Windows CI: after a real client has completed
// a request and the accept loop is waiting for the next named-pipe connection,
// win32PipeListener.Close can otherwise block forever. Exercise the lifecycle
// repeatedly so a regression fails this test's local deadline instead of the
// package-wide ten-minute timeout.
func TestWindowsNamedPipeStopIsBoundedAfterProbe(t *testing.T) {
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprintf("iteration-%02d", i), func(t *testing.T) {
			dir := shortTempDir(t)
			socketPath, heartbeatPath := ServiceEndpoint(dir)
			srv, err := NewServer(ServerOptions{
				SocketPath:    socketPath,
				HeartbeatPath: heartbeatPath,
				Handler:       &fakeHandler{},
			})
			if err != nil {
				t.Fatalf("NewServer: %v", err)
			}
			if err := srv.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}

			client, err := Dial(socketPath, time.Second)
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			if _, err := client.Status(context.Background()); err != nil {
				_ = client.Close()
				t.Fatalf("Status: %v", err)
			}
			if err := client.Close(); err != nil {
				t.Fatalf("client Close: %v", err)
			}

			done := make(chan error, 1)
			go func() { done <- srv.Stop() }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("Stop: %v", err)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("Stop exceeded the bounded Windows named-pipe shutdown")
			}
		})
	}
}
