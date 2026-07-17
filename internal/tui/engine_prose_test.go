package tui

// AST gate over internal/engine: every localizable format/step literal at
// the Msg helper call sites must exist in all four bundles. Sibling of
// options_vocab_test.go. Task 6 adds the drift rule and the count floor.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/i18n"
)

const engineDir = "../engine"

// engineProseKeys parses every non-test .go file in internal/engine and
// returns the set of localizable literals: format args of WithSummary/
// AppendSummary (arg 0), Progressf (args 0 and 1), PromptReq (arg 1), and
// every Progress{Step: "..."} composite-literal step. A NON-literal at any
// of these positions is an error — restructure the call site so every
// branch passes its own literal (dynamic text is an ARG, never the format).
func engineProseKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	fset := token.NewFileSet()
	ents, err := os.ReadDir(engineDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		name := e.Name()
		// msg.go is where WithSummary/AppendSummary/Progressf/PromptReq are
		// DEFINED: Progressf's own "Progress{Step: step}" forwards a caller
		// parameter and can never be a literal — that's not a missing key,
		// it's the mechanism itself. Task 6's TestEngineProseHelperOnly
		// skips msg.go for the same reason.
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "msg.go" {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(engineDir, name), nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				fn := calleeName(node)
				var litArgs []int
				switch fn {
				case "WithSummary", "AppendSummary":
					litArgs = []int{0}
				case "Progressf":
					litArgs = []int{0, 1}
				case "PromptReq":
					litArgs = []int{1}
				default:
					return true
				}
				for _, i := range litArgs {
					if i >= len(node.Args) {
						continue
					}
					s, ok := stringLit(node.Args[i])
					if !ok {
						t.Errorf("%s: %s arg %d must be a string literal (restructure branches; dynamic text is an ARG)",
							fset.Position(node.Pos()), fn, i)
						continue
					}
					keys[s] = true
				}
			case *ast.CompositeLit:
				// Progress{Step: "..."} — the step vocabulary.
				id, ok := node.Type.(*ast.Ident)
				if !ok || id.Name != "Progress" {
					return true
				}
				for _, el := range node.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					k, ok := kv.Key.(*ast.Ident)
					if !ok || k.Name != "Step" {
						continue
					}
					if s, ok := stringLit(kv.Value); ok {
						keys[s] = true
					} else {
						t.Errorf("%s: Progress.Step must be a string literal", fset.Position(kv.Pos()))
					}
				}
			}
			return true
		})
	}
	return keys
}

func calleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func TestEngineProseKeysInBundles(t *testing.T) {
	keys := engineProseKeys(t)
	builtins := i18n.Builtins()
	for _, code := range []string{"ja", "ko", "zh", "ru"} {
		b, ok := builtins[code]
		if !ok {
			t.Fatalf("embedded bundle %s missing", code)
		}
		for k := range keys {
			if _, has := b[k]; !has {
				t.Errorf("%s.toml: missing engine-prose key %q", code, k)
			}
		}
	}
}
