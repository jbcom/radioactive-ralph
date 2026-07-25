//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/jbcom/radioactive-ralph/internal/service"
	"golang.org/x/sys/windows/svc"
)

const windowsSupervisorFailureExitCode = 1

type windowsSupervisorService struct {
	run func(context.Context) error
}

// maybeRunWindowsService intercepts an SCM launch before Cobra handles
// --supervisor. An operator-attached `radioactive_ralph --supervisor` stays on
// the ordinary terminal path, preserving signal-driven foreground operation.
func maybeRunWindowsService() (handled bool, exitCode int) {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false, 0
	}

	configPath, err := service.WindowsServiceConfigPath(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "radioactive_ralph: windows service invocation: %v\n", err)
		return true, windowsSupervisorFailureExitCode
	}
	config, err := service.LoadWindowsServiceConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "radioactive_ralph: windows service config: %v\n", err)
		return true, windowsSupervisorFailureExitCode
	}
	if err := applyWindowsServiceEnvironment(config.ExtraEnv); err != nil {
		fmt.Fprintf(os.Stderr, "radioactive_ralph: windows service environment: %v\n", err)
		return true, windowsSupervisorFailureExitCode
	}
	// This marker is set after ExtraEnv so an installed value cannot disguise
	// the process's actual lifecycle host.
	if err := os.Setenv("RALPH_SERVICE_CONTEXT", "1"); err != nil {
		fmt.Fprintf(os.Stderr, "radioactive_ralph: mark windows service context: %v\n", err)
		return true, windowsSupervisorFailureExitCode
	}

	handler := &windowsSupervisorService{
		run: func(ctx context.Context) error {
			return runSupervisorMode(ctx, "text")
		},
	}
	if err := svc.Run(service.UnitName(service.BackendWindowsSCM), handler); err != nil {
		fmt.Fprintf(os.Stderr, "radioactive_ralph: run windows service: %v\n", err)
		return true, windowsSupervisorFailureExitCode
	}
	return true, 0
}

func applyWindowsServiceEnvironment(extraEnv map[string]string) error {
	// Stable order keeps the first invalid entry deterministic if a config was
	// edited outside `service install`.
	keys := make([]string, 0, len(extraEnv))
	for key := range extraEnv {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := os.Setenv(key, extraEnv[key]); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	return nil
}

// Execute implements svc.Handler. The existing supervisor owns all runtime
// cleanup; the SCM adapter supplies a cancellable context and does not return
// until that supervisor has fully drained and closed its store/socket.
func (s *windowsSupervisorService) Execute(
	_ []string,
	requests <-chan svc.ChangeRequest,
	changes chan<- svc.Status,
) (serviceSpecificExitCode bool, exitCode uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.run(ctx)
	}()

	current := svc.Status{State: svc.Running, Accepts: accepts}
	changes <- current

	stopping := false
	for {
		select {
		case err := <-done:
			if !stopping {
				changes <- svc.Status{State: svc.StopPending, WaitHint: 30_000}
			}
			if err != nil {
				return true, windowsSupervisorFailureExitCode
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				changes <- current
			case svc.Stop, svc.Shutdown:
				if !stopping {
					stopping = true
					current = svc.Status{State: svc.StopPending, WaitHint: 30_000}
					changes <- current
					cancel()
				}
			}
		}
	}
}
