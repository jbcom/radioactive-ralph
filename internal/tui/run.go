package tui

import (
	"context"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

// Options configures Run.
type Options struct {
	// ProjectID scopes the macro view's plan list + event feed. Empty
	// lists across all projects.
	ProjectID string

	// Output/Input override the terminal streams tea.Program attaches
	// to; nil defaults to os.Stdout/os.Stdin. Tests that DO drive a real
	// tea.Program (none currently do — model_test.go drives Update
	// directly) would use this to avoid touching the real terminal.
	Output io.Writer
	Input  io.Reader
}

// Run starts the read-only TUI against source and blocks until the
// operator quits or ctx is cancelled. Callers MUST verify stdout is a
// real terminal before calling Run (see IsTerminal) — Run itself does not
// guard against a non-tty stdout, so that a caller wanting to force a run
// against a pseudo-tty in an integration test still can.
func Run(ctx context.Context, source DataSource, opts Options) error {
	m := NewModel(ctx, source, opts.ProjectID)

	teaOpts := []tea.ProgramOption{tea.WithContext(ctx)}
	if opts.Output != nil {
		teaOpts = append(teaOpts, tea.WithOutput(opts.Output))
	}
	if opts.Input != nil {
		teaOpts = append(teaOpts, tea.WithInput(opts.Input))
	}

	p := tea.NewProgram(m, teaOpts...)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: run: %w", err)
	}
	return nil
}

// IsTerminal reports whether stdout is attached to a real terminal. The
// client entry point (cmd/radioactive_ralph) MUST check this before
// calling Run: launching the Bubble Tea program against a pipe or in a
// non-interactive CI job would hang waiting for terminal input/output
// that will never arrive. When it returns false, callers should fall back
// to a plain status print instead of Run.
func IsTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}
