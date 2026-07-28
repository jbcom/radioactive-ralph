package ipc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestEveryDriveCommandIsRoutedByHandleConn is a STRUCTURAL guard, not a
// per-command test, because this bug class has now bitten twice.
//
// Drive commands are named in two places: the allow-list in handleConn that
// decides what reaches dispatchDrive, and dispatchDrive's own switch that
// handles them. Adding a command to the second alone compiles, passes review,
// and is rejected at runtime as an unknown command — the failure appears ~100
// lines away from the code that looks correct.
//
// Parsing the source is deliberate. A test that restated the command list would
// need updating in a THIRD place, which is the same failure mode one level up.
func TestEveryDriveCommandIsRoutedByHandleConn(t *testing.T) {
	// Every dispatcher gets the same guard. A NEW command surface is the same
	// bug class one level up: adding dispatchCalibration and forgetting the
	// handleConn case compiles and is rejected at runtime, exactly as adding a
	// drive command to one switch alone was.
	for _, dispatcher := range []string{"dispatchDrive", "dispatchCalibration"} {
		t.Run(dispatcher, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "server.go", nil, 0)
			if err != nil {
				t.Fatalf("parse server.go: %v", err)
			}

			routed := caseNamesInSwitchCalling(t, file, dispatcher)
			handled := caseNamesInFunc(t, file, dispatcher)

			if len(routed) == 0 || len(handled) == 0 {
				t.Fatalf("found routed=%d handled=%d command cases for %s; the parser "+
					"found neither switch, so this guard is not actually checking anything",
					len(routed), len(handled), dispatcher)
			}
			for name := range handled {
				if !routed[name] {
					t.Errorf("%s handles %s but handleConn never routes it — "+
						"every call is rejected as an unknown command before it arrives",
						dispatcher, name)
				}
			}
			for name := range routed {
				if !handled[name] {
					t.Errorf("handleConn routes %s into %s but %s has no case for it — "+
						"it falls through to the default", name, dispatcher, dispatcher)
				}
			}
		})
	}
}

// caseNamesInSwitchCalling finds the handleConn case clause whose body calls
// the named dispatcher and returns the command identifiers it lists.
func caseNamesInSwitchCalling(t *testing.T, file *ast.File, dispatcher string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		calls := false
		ast.Inspect(clause, func(inner ast.Node) bool {
			call, ok := inner.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == dispatcher {
				calls = true
			}
			return true
		})
		if !calls {
			return true
		}
		for _, expr := range clause.List {
			if ident, ok := expr.(*ast.Ident); ok {
				names[ident.Name] = true
			}
		}
		return true
	})
	return names
}

// caseNamesInFunc returns every Cmd* identifier appearing in a case clause
// inside the named function.
func caseNamesInFunc(t *testing.T, file *ast.File, funcName string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != funcName {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List {
				if ident, ok := expr.(*ast.Ident); ok && len(ident.Name) > 3 && ident.Name[:3] == "Cmd" {
					names[ident.Name] = true
				}
			}
			return true
		})
	}
	return names
}
