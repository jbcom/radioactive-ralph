package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/onboard"
	"github.com/jbcom/radioactive-ralph/internal/service"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/mattn/go-isatty"
)

// errNoSupervisorListening is the sentinel the client returns when it cannot
// reach a supervisor and the user did not (or could not) start one via the
// wizard. Kept as a package var so both the interactive and non-interactive
// paths return the identical error.
var errNoSupervisorListening = errors.New("no supervisor listening")

// noSupervisorMessage is the exact non-interactive message (unchanged from
// before the wizard existed): a pipe/CI/go-test invocation prints this and
// exits non-zero, and tests assert on it.
const noSupervisorMessage = "radioactive_ralph: no supervisor is running.\n" +
	"Install the durable background service:  radioactive_ralph service install\n" +
	"or run one in the foreground (dies with this terminal):  radioactive_ralph --supervisor"

// onboardingInteractive reports whether BOTH stdin and stdout are real
// terminals — the wizard reads keystrokes from stdin and renders prompts to
// stdout, so it must never run when either is redirected (a pipe, CI, or
// `go test`). This is stricter than tui.IsTerminal (stdout only) on purpose.
func onboardingInteractive() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

// runFirstRunWizard builds the real onboarding dependencies and runs the
// guided wizard. It returns (supervisorReady, err): supervisorReady=true means
// the caller should re-discover the now-running supervisor and continue to the
// TUI; false means the user chose the foreground/manual path (the caller
// returns errNoSupervisorListening). A non-nil err is a hard failure.
func runFirstRunWizard(ctx context.Context, stateRoot string) (supervisorReady bool, err error) {
	plan, err := buildOnboardPlan(stateRoot)
	if err != nil {
		return false, err
	}

	deps := onboard.Deps{
		Out:            os.Stdout,
		Prompt:         onboard.NewStdinPrompter(os.Stdin, os.Stdout),
		Plan:           plan,
		InstallService: func() error { return installSupervisorService(stateRoot) },
		WaitReachable: func(timeout time.Duration) bool {
			return waitSupervisorReachable(ctx, stateRoot, timeout)
		},
		ForegroundCmd:  "radioactive_ralph --supervisor",
		ManualCommands: noSupervisorMessage,
	}

	outcome, err := onboard.Run(deps)
	if err != nil {
		return false, err
	}
	return outcome == onboard.SupervisorReady, nil
}

// buildOnboardPlan computes the "what will be created" summary from xdg +
// service, so the consent prompt names concrete paths.
func buildOnboardPlan(stateRoot string) (onboard.Plan, error) {
	backend := service.DetectBackend()
	plan := onboard.Plan{
		StateDir: stateRoot,
		DBPath:   storeDBPath(stateRoot),
	}
	if backend != service.BackendUnsupported {
		home, err := os.UserHomeDir()
		if err != nil {
			// Don't show a broken/empty unit path — degrade to naming the
			// unit without its path rather than presenting a misleading one.
			plan.ServiceUnit = service.UnitName(backend)
		} else {
			plan.ServiceUnit = service.UnitName(backend)
			plan.ServiceUnitPath = service.UnitPath(backend, home)
		}
	}
	return plan, nil
}

// installSupervisorService installs (and, where the platform does so, starts)
// the per-user supervisor service, pointing it at this executable and the
// resolved state root.
func installSupervisorService(stateRoot string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve own executable: %w", err)
	}
	_, err = service.Install(service.InstallOptions{
		RalphBin: exe,
		ExtraEnv: map[string]string{"RALPH_STATE_DIR": stateRoot},
	})
	return err
}

// waitSupervisorReachable polls supervisor.Find until one answers, the
// timeout elapses, or ctx is cancelled (e.g. the user hits Ctrl+C).
func waitSupervisorReachable(ctx context.Context, stateRoot string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false
		}
		if client, err := supervisor.Find(stateRoot); err == nil {
			_ = client.Close()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(250 * time.Millisecond):
		}
	}
	return false
}
