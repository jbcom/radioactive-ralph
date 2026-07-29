package orch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDecisionLogNeverTargetsTheRealStateRoot stops the suite writing into an
// operator's live state.
//
// ~/.local/state/radioactive-ralph/workers accumulated 188 .decisions.md files
// that were NOT from dispatch: their timestamps matched `go test` runs and
// their per-category counts were uniform, the signature of fixtures. Any test
// constructing New(s) without WithDecisionLogRoot falls through to
// defaultDecisionLogRoot -- the REAL XDG root -- and ten do.
//
// Two harms, the second worse. It litters live state, and it SALTS the one
// diagnostic surface an operator would mine: I nearly drew conclusions about a
// production failure from files my own test run had written.
//
// Guarded by refusing the default when the process is a test binary, which is
// checkable without threading a flag through every constructor.
func TestDecisionLogNeverTargetsTheRealStateRoot(t *testing.T) {
	s := newTestStore(t)

	// No WithDecisionLogRoot: exactly the ten call sites that caused this.
	o := New(s)
	path, err := o.decisionLogPath("worker-x")
	if err != nil {
		// Refusing outright is an acceptable outcome; writing to the real root
		// is not.
		return
	}

	home, herr := os.UserHomeDir()
	if herr != nil {
		t.Skip("no home dir")
	}
	realRoot := filepath.Join(home, ".local", "state", "radioactive-ralph")
	if strings.HasPrefix(path, realRoot) {
		t.Errorf("decision log path %q is under the REAL state root %q; running "+
			"the suite writes into an operator's live state and salts the "+
			"diagnostic surface they would read", path, realRoot)
	}
}
