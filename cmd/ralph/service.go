package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/service"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// ServiceCmd is `ralph service <subcommand>`.
type ServiceCmd struct {
	Install   ServiceInstallCmd   `cmd:"" help:"Install a per-user service unit for a variant."`
	Uninstall ServiceUninstallCmd `cmd:"" help:"Remove a per-user service unit."`
	List      ServiceListCmd      `cmd:"" help:"List installed service units."`
	Status    ServiceStatusCmd    `cmd:"" help:"Shell out to launchctl/systemctl for live service status."`
}

// ServiceInstallCmd wires the service.Install filesystem operation.
type ServiceInstallCmd struct {
	Variant               string   `help:"Variant to install." required:""`
	RepoRoot              string   `help:"Repo root. Defaults to cwd." type:"path"`
	RalphBin              string   `help:"Absolute path to ralph binary. Defaults to the currently-running executable."`
	ConfirmBurnBudget     bool     `help:"Confirmation gate for savage." name:"confirm-burn-budget"`
	ConfirmNoMercy        bool     `help:"Confirmation gate for old-man." name:"confirm-no-mercy"`
	ConfirmBurnEverything bool     `help:"Confirmation gate for world-breaker." name:"confirm-burn-everything"`
	Env                   []string `help:"KEY=VALUE env vars to inject into the service unit (repeatable)."`
}

// Run installs the unit.
func (c *ServiceInstallCmd) Run(_ *runContext) error {
	repo, err := resolveRepoRoot(c.RepoRoot)
	if err != nil {
		return err
	}
	p, err := variant.Lookup(c.Variant)
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

	gate := c.gateConfirmed(p)

	extraEnv := map[string]string{}
	for _, kv := range c.Env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("malformed --env entry %q (want KEY=VALUE)", kv)
		}
		extraEnv[k] = v
	}

	path, err := service.Install(service.InstallOptions{
		RalphBin:      bin,
		RepoPath:      repo,
		Variant:       p,
		GateConfirmed: gate,
		ExtraEnv:      extraEnv,
	})
	if err != nil {
		return err
	}
	fmt.Printf("installed %s\n", path)
	return nil
}

func (c *ServiceInstallCmd) gateConfirmed(p variant.Profile) bool {
	switch p.ConfirmationGate {
	case "--confirm-burn-budget":
		return c.ConfirmBurnBudget
	case "--confirm-no-mercy":
		return c.ConfirmNoMercy
	case "--confirm-burn-everything":
		return c.ConfirmBurnEverything
	}
	return true // non-gated variants always pass the check
}

// ServiceUninstallCmd removes an installed unit.
type ServiceUninstallCmd struct {
	Variant  string `help:"Variant to remove." required:""`
	RepoRoot string `help:"Repo root. Defaults to cwd." type:"path"`
}

// Run removes the unit.
func (c *ServiceUninstallCmd) Run(_ *runContext) error {
	p, err := variant.Lookup(c.Variant)
	if err != nil {
		return err
	}
	if err := service.Uninstall(service.InstallOptions{Variant: p}); err != nil {
		return err
	}
	fmt.Printf("uninstalled %s\n", service.UnitName(service.DetectBackend(), p.Name))
	return nil
}

// ServiceListCmd lists installed units.
type ServiceListCmd struct{}

// Run shells out to launchctl/systemctl to enumerate installed units.
// M2 implementation is simple: list the unit files under the per-user
// config dir and report their presence. Status is available via the
// dedicated status subcommand.
func (c *ServiceListCmd) Run(_ *runContext) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	backend := service.DetectBackend()
	for _, vn := range []variant.Name{
		variant.Blue, variant.Grey, variant.Green, variant.Red,
		variant.Professor, variant.Fixit, variant.Immortal,
		variant.Savage, variant.OldMan, variant.WorldBreaker,
	} {
		path := service.UnitPath(backend, home, vn)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("installed  %s  %s\n", vn, path)
		}
	}
	return nil
}

// ServiceStatusCmd invokes the platform service manager for live status.
type ServiceStatusCmd struct {
	Variant string `help:"Variant to query." required:""`
}

// Run shells out to launchctl list / systemctl --user status.
func (c *ServiceStatusCmd) Run(_ *runContext) error {
	p, err := variant.Lookup(c.Variant)
	if err != nil {
		return err
	}
	backend := service.DetectBackend()
	name := service.UnitName(backend, p.Name)
	var cmd *exec.Cmd
	switch backend {
	case service.BackendLaunchd:
		cmd = exec.Command("launchctl", "list", name) //nolint:gosec // args are fixed
	case service.BackendSystemdUser:
		cmd = exec.Command("systemctl", "--user", "status", name+".service") //nolint:gosec // args are fixed
	default:
		return fmt.Errorf("service status not supported on this platform")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
