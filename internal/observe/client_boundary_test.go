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

	for _, relativeFile := range []string{
		"cmd/radioactive_ralph/events_cmd.go",
		"cmd/radioactive_ralph/gui_cmd.go",
		"cmd/radioactive_ralph/query_cmd.go",
	} {
		assertFileDoesNotImportStore(
			t,
			root,
			filepath.Join(root, relativeFile),
		)
	}
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
