package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/contain"
)

// TestMain makes this test binary answer a containment-helper re-invocation,
// exactly as cmd/radioactive_ralph/main.go does.
//
// On Linux, contain.Policy.Wrap confines a command by re-execing os.Executable()
// with a helper flag; the re-invoked process applies the Landlock restriction
// and then execs the real target. When containment runs IN-PROCESS from a test,
// os.Executable() is this test binary — so without this hook it receives the
// helper flag, does not recognize it, and fails.
//
// The failure that produces is the dangerous kind, not an obvious one. The
// contained turn simply never completes, so a test asserting "a real turn
// survives containment" reports that containment broke it — a conclusion about
// the product drawn entirely from a gap in the harness. That exact mistake
// already happened once on this test (the isolated HOME stripping provider
// credentials, #276), and it was only caught by running an uncontained control.
//
// Runs FIRST, before any test setup, for the same reason main() calls it before
// flags and logging: a helper re-invocation exists only to apply its sandbox and
// exec, so anything done beforehand is either thrown away by the exec or escapes
// the restriction.
func TestMain(m *testing.M) {
	if handled, err := contain.MaybeRunHelper(os.Args); handled {
		if err != nil {
			fmt.Fprintf(os.Stderr, "e2e: containment helper: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
