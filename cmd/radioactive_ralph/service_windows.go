//go:build windows

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/plandag"
	runtimecmd "github.com/jbcom/radioactive-ralph/internal/runtime"
	"github.com/jbcom/radioactive-ralph/internal/service"
	"golang.org/x/sys/windows/svc"
)

type ServiceWindowsRunCmd struct {
	RepoRoot    string `help:"Repo root." required:"" type:"path"`
	ServiceName string `help:"Windows service name. Defaults to the repo-derived unit name."`
	ConfigPath  string `help:"Path to the generated Windows service config." type:"path"`
}

func (c *ServiceWindowsRunCmd) Run(_ *runContext) error {
	repo, err := resolveRepoRoot(c.RepoRoot)
	if err != nil {
		return err
	}
	if c.ConfigPath != "" {
		if err := applyWindowsServiceEnv(c.ConfigPath); err != nil {
			return err
		}
	}
	name := c.ServiceName
	if name == "" {
		name = service.UnitName(service.BackendWindowsSCM, repo)
	}
	return runWindowsService(name, repo)
}

func applyWindowsServiceEnv(path string) error {
	raw, err := os.ReadFile(path) //nolint:gosec // service-owned path
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	cfg, err := service.ParseWindowsServiceConfig(raw)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	for k, v := range cfg.ExtraEnv {
		if err := os.Setenv(k, v); err != nil {
			return fmt.Errorf("set %s: %w", k, err)
		}
	}
	return nil
}

func runWindowsService(name, repo string) error {
	if err := os.Setenv("RALPH_SERVICE_CONTEXT", "1"); err != nil {
		return fmt.Errorf("set RALPH_SERVICE_CONTEXT: %w", err)
	}
	return svc.Run(name, &windowsServiceHandler{repo: repo})
}

type windowsServiceHandler struct {
	repo string
}

func (h *windowsServiceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		svcRuntime, err := runtimecmd.NewService(runtimecmd.Options{
			RepoPath:         h.repo,
			SessionMode:      plandag.SessionModeDurable,
			SessionTransport: plandag.SessionTransportSocket,
		})
		if err != nil {
			errCh <- err
			return
		}
		errCh <- svcRuntime.Run(ctx)
	}()

	status <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				err := <-errCh
				if err != nil {
					return false, 1
				}
				status <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		case err := <-errCh:
			if err != nil {
				return false, 1
			}
			status <- svc.Status{State: svc.Stopped}
			return false, 0
		}
	}
}
