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
	return runWindowsServiceHost(windowsServiceHostArgs{
		RepoRoot:    repo,
		ServiceName: c.ServiceName,
		ConfigPath:  c.ConfigPath,
	})
}

func maybeRunWindowsServiceHost(args []string) (bool, error) {
	parsed, handled, err := parseWindowsServiceHostArgs(args)
	if !handled || err != nil {
		return handled, err
	}
	return true, runWindowsServiceHost(parsed)
}

func applyWindowsServiceEnv(path string) (service.WindowsServiceConfig, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // service-owned path
	if err != nil {
		return service.WindowsServiceConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	cfg, err := service.ParseWindowsServiceConfig(raw)
	if err != nil {
		return service.WindowsServiceConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	for k, v := range cfg.ExtraEnv {
		if err := os.Setenv(k, v); err != nil {
			return service.WindowsServiceConfig{}, fmt.Errorf("set %s: %w", k, err)
		}
	}
	return cfg, nil
}

func runWindowsServiceHost(args windowsServiceHostArgs) error {
	name := args.ServiceName
	if name == "" && args.RepoRoot != "" {
		name = service.UnitName(service.BackendWindowsSCM, args.RepoRoot)
	}
	if name == "" {
		return fmt.Errorf("service name required")
	}
	if err := os.Setenv("RALPH_SERVICE_CONTEXT", "1"); err != nil {
		return fmt.Errorf("set RALPH_SERVICE_CONTEXT: %w", err)
	}
	return svc.Run(name, &windowsServiceHandler{
		repo:       args.RepoRoot,
		configPath: args.ConfigPath,
	})
}

type windowsServiceHandler struct {
	repo       string
	configPath string
}

func (h *windowsServiceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	repo := h.repo
	if h.configPath != "" {
		cfg, err := applyWindowsServiceEnv(h.configPath)
		if err != nil {
			return false, 1
		}
		if cfg.RepoPath != "" {
			repo = cfg.RepoPath
		}
	}
	if repo == "" {
		return false, 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		svcRuntime, err := runtimecmd.NewService(runtimecmd.Options{
			RepoPath:         repo,
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
