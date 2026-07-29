package main

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestSupervisorFlagNamesItsConcurrencyControl keeps the dispatch width
// discoverable from the CLI.
//
// RALPH_MAX_PARALLEL governs how many worker turns run at once, and unset means
// UNBOUNDED. That default is what starved a long quiet step during a real
// self-test: not a tuned budget the step lost against, but however many siblings
// the dependency graph happened to release at once.
//
// It was documented only in design docs. An operator debugging a reclaimed task
// reads `--help`, not docs/design/config-layers.md, so the one knob that
// explains the behaviour was invisible exactly where it was needed.
func TestSupervisorFlagNamesItsConcurrencyControl(t *testing.T) {
	root := newRootCmd(context.Background(), func(context.Context, *cobra.Command) (bool, error) { return false, nil })
	flag := root.Flags().Lookup("supervisor")
	if flag == nil {
		t.Fatal("no --supervisor flag")
	}
	if !strings.Contains(flag.Usage, maxParallelEnv) {
		t.Errorf("--supervisor usage does not name %s:\n%s", maxParallelEnv, flag.Usage)
	}
	// The DEFAULT is the surprising part and the part worth stating: a reader
	// who assumes a sane built-in cap gets an unbounded one.
	if !strings.Contains(strings.ToUpper(flag.Usage), "UNBOUNDED") {
		t.Errorf("--supervisor usage does not say the unset default is unbounded:\n%s", flag.Usage)
	}
}
