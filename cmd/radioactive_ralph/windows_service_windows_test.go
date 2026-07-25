//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
)

type windowsServiceResult struct {
	serviceSpecific bool
	exitCode        uint32
}

func TestWindowsSupervisorServiceStopCancelsAndDrains(t *testing.T) {
	cancelled := make(chan struct{})
	release := make(chan struct{})
	handler := &windowsSupervisorService{
		run: func(ctx context.Context) error {
			<-ctx.Done()
			close(cancelled)
			<-release
			return nil
		},
	}
	requests := make(chan svc.ChangeRequest, 2)
	changes := make(chan svc.Status, 4)
	result := make(chan windowsServiceResult, 1)
	go func() {
		specific, code := handler.Execute(nil, requests, changes)
		result <- windowsServiceResult{serviceSpecific: specific, exitCode: code}
	}()

	if status := receiveWindowsStatus(t, changes); status.State != svc.StartPending {
		t.Fatalf("first state = %d, want StartPending", status.State)
	}
	if status := receiveWindowsStatus(t, changes); status.State != svc.Running ||
		status.Accepts != svc.AcceptStop|svc.AcceptShutdown {
		t.Fatalf("running status = %+v", status)
	}
	requests <- svc.ChangeRequest{Cmd: svc.Stop}
	if status := receiveWindowsStatus(t, changes); status.State != svc.StopPending {
		t.Fatalf("stop state = %d, want StopPending", status.State)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("SCM Stop did not cancel the supervisor context")
	}
	select {
	case got := <-result:
		t.Fatalf("handler returned before supervisor drained: %+v", got)
	default:
	}
	close(release)
	select {
	case got := <-result:
		if got.serviceSpecific || got.exitCode != 0 {
			t.Fatalf("clean stop result = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not return after supervisor drained")
	}
}

func TestWindowsSupervisorServiceReportsSupervisorFailure(t *testing.T) {
	handler := &windowsSupervisorService{
		run: func(context.Context) error {
			return errors.New("store unavailable")
		},
	}
	requests := make(chan svc.ChangeRequest)
	changes := make(chan svc.Status, 4)
	result := make(chan windowsServiceResult, 1)
	go func() {
		specific, code := handler.Execute(nil, requests, changes)
		result <- windowsServiceResult{serviceSpecific: specific, exitCode: code}
	}()

	_ = receiveWindowsStatus(t, changes)
	_ = receiveWindowsStatus(t, changes)
	select {
	case got := <-result:
		if !got.serviceSpecific || got.exitCode != windowsSupervisorFailureExitCode {
			t.Fatalf("failure result = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not report supervisor failure")
	}
}

func TestApplyWindowsServiceEnvironment(t *testing.T) {
	const key = "RALPH_WINDOWS_SERVICE_TEST_ENV"
	t.Setenv(key, "before")
	if err := applyWindowsServiceEnvironment(map[string]string{key: "after"}); err != nil {
		t.Fatalf("applyWindowsServiceEnvironment: %v", err)
	}
	if got := os.Getenv(key); got != "after" {
		t.Fatalf("%s = %q, want after", key, got)
	}
}

func receiveWindowsStatus(t *testing.T, changes <-chan svc.Status) svc.Status {
	t.Helper()
	select {
	case status := <-changes:
		return status
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for service status")
		return svc.Status{}
	}
}
