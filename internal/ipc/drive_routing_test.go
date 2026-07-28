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
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "server.go", nil, 0)
	if err != nil {
		t.Fatalf("parse server.go: %v", err)
	}

	routed := caseNamesInSwitchCallingDispatchDrive(t, file)
	handled := caseNamesInFunc(t, file, "dispatchDrive")

	if len(routed) == 0 || len(handled) == 0 {
		t.Fatalf("found routed=%d handled=%d command cases; the parser found "+
			"neither switch, so this guard is not actually checking anything",
			len(routed), len(handled))
	}
	for name := range handled {
		if !routed[name] {
			t.Errorf("dispatchDrive handles %s but handleConn never routes it — "+
				"every call is rejected as an unknown command before it arrives", name)
		}
	}
	for name := range routed {
		if !handled[name] {
			t.Errorf("handleConn routes %s into dispatchDrive but dispatchDrive "+
				"has no case for it — it falls through to the default", name)
		}
	}
}

// caseNamesInSwitchCallingDispatchDrive finds the handleConn case clause whose
// body calls dispatchDrive and returns the command identifiers it lists.
func caseNamesInSwitchCallingDispatchDrive(t *testing.T, file *ast.File) map[string]bool {
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
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "dispatchDrive" {
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
