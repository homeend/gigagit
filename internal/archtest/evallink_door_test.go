package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFrontendsReachLinkSetsThroughTheDoor: a frontend never calls
// domain.EvalLink itself. EvalLink has a PRECONDITION — the link is already
// located, because a parsed local-form link holds its checkout and its file
// undivided and only a locate splits them — and a caller that skips it gets a
// whole-tree answer for a file link, with no error. MCP shipped exactly that.
// domain.EvalLinkText / CompareLinks are the door that cannot forget.
func TestFrontendsReachLinkSetsThroughTheDoor(t *testing.T) {
	for _, pkg := range []string{"tui", "cli", "mcp", "web"} {
		dir := filepath.Join("..", pkg)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "EvalLink" {
					t.Errorf("%s calls EvalLink directly — go through domain.EvalLinkText or domain.CompareLinks, which locate the link first",
						fset.Position(call.Pos()))
				}
				return true
			})
		}
	}
}
