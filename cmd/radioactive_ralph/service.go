package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/plandag"
	runtimecmd "github.com/jbcom/radioactive-ralph/internal/runtime"
	"github.com/jbcom/radioactive-ralph/internal/service"
)

// ServiceCmd is `radioactive_ralph service <subcommand>`.
type ServiceCmd struct {
	Start      ServiceStartCmd      `cmd:"" help:"Launch the durable repo service in the foreground."`
	Install    ServiceInstallCmd    `cmd:"" help:"Install the repo service definition for the current platform."`
	Uninstall  ServiceUninstallCmd  `cmd:"" help:"Remove the repo service definition for the current platform."`
	List       ServiceListCmd       `cmd:"" help:"List installed repo service definitions."`
	Status     ServiceStatusCmd     `cmd:"" help:"Show service-manager status for the current platform."`
	WindowsRun ServiceWindowsRunCmd `cmd:"run-windows" help:"Run as a native Windows service host." hidden:""`
}

// ServiceStartCmd runs the durable repo-scoped runtime.
type ServiceStartCmd struct {
	RepoRoot   string `help:"Repo root. Defaults to cwd." type:"path"`
	Foreground bool   `help:"Run in the foreground. Service units pass this explicitly." hidden:""`
}

func (c *ServiceStartCmd) Run(rc *runContext) error {
	repo, err := resolveRepoRoot(c.RepoRoot)
	if err != nil {
		return err
	}
	svc, err := runtimecmd.NewService(runtimecmd.Options{
		RepoPath:         repo,
		SessionMode:      plandag.SessionModeDurable,
		SessionTransport: plandag.SessionTransportSocket,
	})
	if err != nil {
		return fmt.Errorf("runtime.NewService: %w", err)
	}
	fmt.Printf("radioactive_ralph: durable repo service starting in %s\n", repo)
	return svc.Run(rc.ctx)
}

// ServiceInstallCmd wires the service.Install filesystem operation.
type ServiceInstallCmd struct {
	RepoRoot string   `help:"Repo root. Defaults to cwd." type:"path"`
	RalphBin string   `help:"Absolute path to the radioactive_ralph binary. Defaults to the currently-running executable." name:"radioactive_ralph-bin"`
	Env      []string `help:"KEY=VALUE env vars to inject into the service unit (repeatable)."`
}

func (c *ServiceInstallCmd) Run(_ *runContext) error {
	repo, err := resolveRepoRoot(c.RepoRoot)
	if err != nil {
		return err
	}
	bin := c.RalphBin
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve own binary: %w", err)
		}
		bin = exe
	}

	extraEnv := map[string]string{}
	for _, kv := range c.Env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("malformed --env entry %q (want KEY=VALUE)", kv)
		}
		extraEnv[k] = v
	}

	path, err := service.Install(service.InstallOptions{
		RalphBin: bin,
		RepoPath: repo,
		ExtraEnv: extraEnv,
	})
	if err != nil {
		return err
	}
	fmt.Printf("installed %s\n", path)
	return nil
}

// ServiceUninstallCmd removes an installed unit.
type ServiceUninstallCmd struct {
	RepoRoot string `help:"Repo root. Defaults to cwd." type:"path"`
}

func (c *ServiceUninstallCmd) Run(_ *runContext) error {
	repo, err := resolveRepoRoot(c.RepoRoot)
	if err != nil {
		return err
	}
	if err := service.Uninstall(service.InstallOptions{RepoPath: repo}); err != nil {
		return err
	}
	fmt.Printf("uninstalled %s\n", service.UnitName(service.DetectBackend(), repo))
	return nil
}

// ServiceListCmd lists installed repo service definitions.
type ServiceListCmd struct{}

func (c *ServiceListCmd) Run(_ *runContext) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	backend := service.DetectBackend()
	pattern, err := serviceListPattern(backend, home)
	if err != nil {
		return err
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, path := range matches {
		fmt.Printf("installed  %s\n", path)
	}
	return nil
}

// ServiceStatusCmd invokes the platform service manager for live status.
type ServiceStatusCmd struct {
	RepoRoot string `help:"Repo root. Defaults to cwd." type:"path"`
}

func (c *ServiceStatusCmd) Run(_ *runContext) error {
	repo, err := resolveRepoRoot(c.RepoRoot)
	if err != nil {
		return err
	}
	backend := service.DetectBackend()
	name, args, err := serviceStatusInvocation(backend, repo)
	if err != nil {
		return err
	}
	cmd := exec.Command(name, args...) //nolint:gosec // args are fixed
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func serviceListPattern(backend service.Backend, home string) (string, error) {
	switch backend {
	case service.BackendLaunchd:
		return filepath.Join(home, "Library", "LaunchAgents", "jbcom.radioactive-ralph.*.plist"), nil
	case service.BackendSystemdUser:
		return filepath.Join(home, ".config", "systemd", "user", "radioactive_ralph-*.service"), nil
	case service.BackendWindowsSCM:
		return filepath.Join(home, "AppData", "Local", "radioactive-ralph", "services", "radioactive_ralph-*.json"), nil
	default:
		return "", fmt.Errorf("service list not supported on this platform")
	}
}

func serviceStatusInvocation(backend service.Backend, repo string) (string, []string, error) {
	name := service.UnitName(backend, repo)
	switch backend {
	case service.BackendLaunchd:
		return "launchctl", []string{"list", name}, nil
	case service.BackendSystemdUser:
		return "systemctl", []string{"--user", "status", name + ".service"}, nil
	case service.BackendWindowsSCM:
		return "sc.exe", []string{"query", name}, nil
	default:
		return "", nil, fmt.Errorf("service status not supported on this platform")
	}
}
