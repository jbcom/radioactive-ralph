// Package onboard implements the guided first-run wizard: when a user runs
// the client cold on an interactive terminal and no supervisor is reachable,
// it OFFERS (with explicit consent) to install and start the background
// service, falling back to a foreground supervisor or the plain
// print-the-commands path. See
// docs/superpowers/specs/2026-07-17-guided-first-run-onboarding-design.md.
//
// The wizard is pure orchestration: every side effect (prompting, installing
// the service, discovering the supervisor) is an injected dependency, so the
// whole decision tree is unit-testable without touching the real system.
package onboard

import (
	"errors"
	"fmt"
	"io"
	"time"
)

// Outcome reports how the wizard ended so the caller knows whether to proceed
// to the TUI.
type Outcome int

const (
	// SupervisorReady means a supervisor is now reachable — proceed to the TUI.
	SupervisorReady Outcome = iota
	// PrintedForegroundHint means the wizard printed the `--supervisor`
	// command for the user to run themselves; the caller should exit cleanly.
	PrintedForegroundHint
	// PrintedCommands means the wizard fell back to printing the manual
	// commands; the caller returns its usual no-supervisor error.
	PrintedCommands
)

// Plan is the "what will be created" summary shown before any outward-facing
// action, so the user consents to concrete paths, not a vague promise.
type Plan struct {
	StateDir        string
	DBPath          string
	ServiceUnit     string
	ServiceUnitPath string
}

// Prompter asks the user a yes/no question. Confirm returns (true, nil) for
// yes, (false, nil) for no, and a non-nil error to abort the whole wizard
// (e.g. the user typed 'q' to quit, or stdin closed).
type Prompter interface {
	Confirm(question string, defaultYes bool) (bool, error)
}

// ErrQuit is returned by a Prompter when the user asks to quit the wizard.
var ErrQuit = errors.New("onboard: user quit")

// Deps are the injected dependencies. Every field is required.
type Deps struct {
	// Out receives the wizard's human-facing output.
	Out io.Writer
	// Prompt drives the yes/no questions.
	Prompt Prompter
	// Plan describes what installing the service will create.
	Plan Plan
	// InstallService installs AND (best-effort) starts the background service.
	InstallService func() error
	// WaitReachable blocks until a supervisor answers or the timeout elapses,
	// returning true when one became reachable.
	WaitReachable func(timeout time.Duration) bool
	// ForegroundHint is the exact command a user runs to start a foreground
	// supervisor (printed on the fallback path).
	ForegroundCmd string
	// ManualCommands is the multi-line fallback text (the pre-wizard message).
	ManualCommands string
}

// readyTimeout bounds how long the wizard waits for a freshly-installed
// service to actually come up before treating the install as not-yet-effective
// and offering the foreground fallback.
const readyTimeout = 10 * time.Second

// Run drives the first-run flow. It assumes the caller already confirmed the
// session is interactive and no supervisor is currently reachable.
func Run(d Deps) (Outcome, error) {
	_, _ = fmt.Fprintln(d.Out, "No supervisor is running yet. Ralph can set this up for you:")
	_, _ = fmt.Fprintf(d.Out, "  • state dir:  %s\n", d.Plan.StateDir)
	_, _ = fmt.Fprintf(d.Out, "  • database:   %s\n", d.Plan.DBPath)
	if d.Plan.ServiceUnit != "" {
		_, _ = fmt.Fprintf(d.Out, "  • service:    %s (%s)\n", d.Plan.ServiceUnit, d.Plan.ServiceUnitPath)
	}
	_, _ = fmt.Fprintln(d.Out)

	install, err := d.Prompt.Confirm("Install the background service and start it now?", true)
	if err != nil {
		if errors.Is(err, ErrQuit) {
			return printManual(d), nil
		}
		return PrintedCommands, err
	}

	if install {
		// An install failure is a SOFT failure by design: on a locked-down
		// machine (no launchd/systemd/SCM access) we don't abort — we show
		// the reason and route the user to the foreground fallback. The error
		// is surfaced to the user, not propagated as a hard Run error.
		if installErr := d.InstallService(); installErr != nil {
			_, _ = fmt.Fprintf(d.Out, "\nCould not install the service: %v\n", installErr)
			return offerForeground(d)
		}
		_, _ = fmt.Fprintln(d.Out, "\nService installed — waiting for the supervisor to come up...")
		if d.WaitReachable(readyTimeout) {
			_, _ = fmt.Fprintln(d.Out, "Supervisor is up.")
			return SupervisorReady, nil
		}
		_, _ = fmt.Fprintln(d.Out, "The service was installed but the supervisor is not reachable yet.")
		return offerForeground(d)
	}

	return offerForeground(d)
}

// offerForeground asks whether to run a foreground supervisor; if yes it
// prints the command (leaving the user to run it) and if no falls back to the
// manual-commands path.
func offerForeground(d Deps) (Outcome, error) {
	fg, err := d.Prompt.Confirm("Run a foreground supervisor yourself instead? (it dies with the terminal)", false)
	if err != nil {
		if errors.Is(err, ErrQuit) {
			return printManual(d), nil
		}
		return PrintedCommands, err
	}
	if fg {
		_, _ = fmt.Fprintf(d.Out, "\nStart one in another terminal with:\n  %s\n", d.ForegroundCmd)
		return PrintedForegroundHint, nil
	}
	return printManual(d), nil
}

func printManual(d Deps) Outcome {
	if d.ManualCommands != "" {
		_, _ = fmt.Fprintln(d.Out)
		_, _ = fmt.Fprintln(d.Out, d.ManualCommands)
	}
	return PrintedCommands
}
