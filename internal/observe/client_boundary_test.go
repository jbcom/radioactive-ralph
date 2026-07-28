package observe_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const storeImportPath = "github.com/jbcom/radioactive-ralph/internal/store"

// TestOperatorClientsDoNotImportStore is a source-level architecture gate.
// Production presentation clients must use the versioned supervisor query
// protocol, never SQLite or raw store DTOs. Tests may import store to seed an
// integration fixture because they are not shipped client code.
func TestOperatorClientsDoNotImportStore(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	for _, relativeDir := range []string{"internal/tui", "internal/gui"} {
		dir := filepath.Join(root, relativeDir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%s): %v", relativeDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() ||
				filepath.Ext(entry.Name()) != ".go" ||
				strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			assertFileDoesNotImportStore(
				t,
				root,
				filepath.Join(dir, entry.Name()),
			)
		}
	}

	// The CLI is gated as a WHOLE DIRECTORY with a named exemption list, not as
	// a list of files known to comply. An allowlist of compliant files silently
	// permits every file nobody remembered to add — a new command importing
	// store would pass. Inverting it means a new violation fails this test, and
	// clearing the boundary means deleting an entry here.
	cliDir := filepath.Join(root, "cmd", "radioactive_ralph")
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		t.Fatalf("ReadDir(cmd/radioactive_ralph): %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() ||
			filepath.Ext(entry.Name()) != ".go" ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(cliDir, entry.Name())
		if reason, exempt := clientBoundaryExemptions[entry.Name()]; exempt {
			// An exemption that no longer needs to exist is worse than no
			// exemption: it keeps a file permanently ungated after the debt is
			// paid, so a LATER regression in that file goes unnoticed. Fail when
			// an exempt file has stopped importing store, so clearing the debt
			// forces deleting the entry.
			if !fileImportsStore(t, path) {
				t.Errorf("%s no longer imports store — delete its entry from "+
					"clientBoundaryExemptions (%s)", entry.Name(), reason)
			}
			continue
		}
		assertFileDoesNotImportStore(t, root, path)
	}

	// Likewise, an entry naming a file that no longer exists is dead weight
	// that hides the true size of the remaining debt.
	present := map[string]bool{}
	for _, entry := range entries {
		present[entry.Name()] = true
	}
	for name := range clientBoundaryExemptions {
		if !present[name] {
			t.Errorf("clientBoundaryExemptions names %q, which does not exist in "+
				"cmd/radioactive_ralph — delete the entry", name)
		}
	}
}

// fileImportsStore reports whether path imports the durable store.
func fileImportsStore(t *testing.T, path string) bool {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import in %s: %v", path, err)
		}
		if importPath == storeImportPath {
			return true
		}
	}
	return false
}

// clientBoundaryExemptions names the CLI files that still reach the store
// directly, each with the reason it has not moved behind the supervisor yet.
// Every entry is a known debt, not an approved design — removing one is the
// definition of done for that file.
var clientBoundaryExemptions = map[string]string{
	// SUPERVISOR-SIDE, not clients. These run inside (or directly construct)
	// the supervisor process, which owns the store by design. They are exempt
	// permanently, not pending work.
	"supervisor_cmd.go":   "runs the supervisor process; it IS the store owner",
	"binding_resolver.go": "resolves bindings for the supervisor's orchestrator",

	// REAL DEBT (#204): NONE REMAINING. Every CLI client now reaches the store
	// through the supervisor. init_cmd.go was the last one, and desktop_launch.go
	// was cleared by #246 (non-mutating ProjectEnsure at protocol v4).
	//
	// This section is deliberately kept, empty, rather than deleted: it is where
	// a NEW exemption would go, and the gate fails on any entry whose file has
	// stopped importing store — so an exemption added here has to be retired
	// when its debt is paid.
}

func assertFileDoesNotImportStore(t *testing.T, root, path string) {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import in %s: %v", path, err)
		}
		if importPath == storeImportPath {
			position := fileSet.Position(spec.Pos())
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				relative = path
			}
			t.Errorf(
				"%s:%d imports the durable store; operator clients must query the supervisor",
				relative,
				position.Line,
			)
		}
	}
}
