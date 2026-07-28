// Command containhelper is a minimal stand-in for the real CLI entry point,
// used by internal/contain's behavioral tests.
//
// The tests must run a binary whose main() actually calls MaybeRunHelper. On
// Linux, Wrap re-execs the CALLING binary as a containment helper, so pointing
// it at the go test binary makes that binary reject the helper flag and never
// run the command — which every "did the file appear?" assertion then reads as
// successful containment. That false pass is precisely what these tests exist
// to rule out.
package main

import (
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/contain"
)

func main() {
	handled, err := contain.MaybeRunHelper(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !handled {
		fmt.Fprintln(os.Stderr, "containhelper: not a helper invocation")
		os.Exit(2)
	}
}
