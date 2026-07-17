package genesis

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/jbcom/radioactive-ralph/internal/plan"
)

// ReviewMode selects how RenderForReview presents a plan document to the
// operator once genesis produces it (spec §11: "headless mode -> emits
// the final markdown plan. TUI mode -> renders that markdown for review
// (scroll, an embedded editor, or open-in-$EDITOR -- both offered)").
type ReviewMode int

const (
	// ReviewHeadless just emits the markdown (to Options.Writer or a
	// file) with no interactive review step.
	ReviewHeadless ReviewMode = iota

	// ReviewEditor hands the document to the operator's $EDITOR (or
	// Options.Editor) via a plain os/exec run, then reads the (possibly
	// edited) file back. This is the "open-in-$EDITOR" option; the TUI's
	// embedded-scroll/textarea option is a Bubble Tea concern that lives
	// in internal/tui, not here -- this package only owns the handoff
	// contract (WriteTempFile / ReadBack below) the TUI's tea.ExecProcess
	// wiring needs.
	ReviewEditor
)

// ReviewOptions configures RenderForReview.
type ReviewOptions struct {
	Mode ReviewMode

	// Writer receives the markdown in ReviewHeadless mode. Defaults to
	// os.Stdout when nil.
	Writer io.Writer

	// Editor overrides the command run in ReviewEditor mode. Defaults to
	// the $EDITOR environment variable, falling back to "vi" if unset.
	Editor string

	// TempFilePath, if set, is used instead of a freshly created temp
	// file for ReviewEditor mode. Primarily for tests that want to
	// inspect the file the "editor" was pointed at.
	TempFilePath string
}

// RenderForReview presents md to the operator per opts.Mode and returns
// the (possibly operator-edited) final markdown plus its plan.Validate
// findings, so callers can surface ambiguities either way. It never
// blocks on ReviewHeadless (a pure emit); ReviewEditor blocks for the
// duration of the external editor process, mirroring how `git commit`
// hands off to $EDITOR.
func RenderForReview(md []byte, opts ReviewOptions) (final []byte, findings []plan.PlanError, err error) {
	switch opts.Mode {
	case ReviewEditor:
		final, err = runEditorReview(md, opts)
		if err != nil {
			return nil, nil, err
		}
	default:
		w := opts.Writer
		if w == nil {
			w = os.Stdout
		}
		if _, err := w.Write(md); err != nil {
			return nil, nil, fmt.Errorf("genesis: write plan for review: %w", err)
		}
		final = md
	}

	return final, plan.Validate(final), nil
}

// runEditorReview writes md to a temp file (or opts.TempFilePath), execs
// the editor against it, and reads the result back. Exported as a plain
// function (rather than folded into a tea.Cmd) so internal/tui can wrap
// it in tea.ExecProcess itself -- this package stays independent of
// Bubble Tea.
func runEditorReview(md []byte, opts ReviewOptions) ([]byte, error) {
	path := opts.TempFilePath
	if path == "" {
		f, err := os.CreateTemp("", "ralph-plan-*.md")
		if err != nil {
			return nil, fmt.Errorf("genesis: create temp plan file: %w", err)
		}
		path = f.Name()
		defer func() { _ = os.Remove(path) }()
		if err := f.Close(); err != nil {
			return nil, fmt.Errorf("genesis: close temp plan file: %w", err)
		}
	}

	if err := os.WriteFile(path, md, 0o600); err != nil {
		return nil, fmt.Errorf("genesis: write temp plan file: %w", err)
	}

	editor := opts.Editor
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	//nolint:gosec // G204: editor comes from EDITOR/opts, the same trust
	// boundary as `git commit`'s use of $EDITOR -- the operator's own
	// configured editor, not untrusted input.
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("genesis: run editor %q: %w", editor, err)
	}

	final, err := os.ReadFile(path) //nolint:gosec // path is our own temp file or an explicit caller-supplied path
	if err != nil {
		return nil, fmt.Errorf("genesis: read back edited plan file: %w", err)
	}
	return final, nil
}
