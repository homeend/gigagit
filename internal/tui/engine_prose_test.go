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
// every Progress{Step: "..."} composite-literal step.
//
// It is a PURE COLLECTOR: a non-literal at any of these positions is
// SKIPPED, not an error. Two consumers depend on that — the bundle-coverage
// check below, and i18n_scan_test.go's used-key union — and both must stay
// green incrementally as the ops migrate wave by wave (a not-yet-migrated
// dynamic Step is simply "not a key yet"). The STRICT rejection of any
// remaining non-literal format/step (the invariant that every engine
// sentence originates as a literal) is armed once, after the migration is
// complete, by TestEngineProseNoDynamic + TestEngineProseHelperOnly +
// TestEngineProseFloor in the gate-completion task.
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
					// Pure collector: a non-literal is skipped here and caught
					// by the strict gate (TestEngineProseNoDynamic) post-migration.
					if s, ok := stringLit(node.Args[i]); ok {
						keys[s] = true
					}
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
					// Pure collector: a non-literal Step is skipped here (it is
					// simply an unmigrated op) and caught by the strict gate.
					if s, ok := stringLit(kv.Value); ok {
						keys[s] = true
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

// TestEngineProseHelperOnly is the drift rule: after stage 5, engine code
// never hand-builds the English channels — a Result composite with a
// Summary: field, any assignment to .Summary, or a DecisionRequest
// composite with a Prompt: field would let the channels drift.
func TestEngineProseHelperOnly(t *testing.T) {
	fset := token.NewFileSet()
	ents, err := os.ReadDir(engineDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "msg.go" {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(engineDir, name), nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CompositeLit:
				id, ok := node.Type.(*ast.Ident)
				if !ok {
					return true
				}
				field := ""
				switch id.Name {
				case "Result":
					field = "Summary"
				case "DecisionRequest":
					field = "Prompt"
				default:
					return true
				}
				for _, el := range node.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == field {
						t.Errorf("%s: hand-built %s.%s — use the msg.go helpers (WithSummary/PromptReq)",
							fset.Position(kv.Pos()), id.Name, field)
					}
				}
			case *ast.AssignStmt:
				for _, lhs := range node.Lhs {
					sel, ok := lhs.(*ast.SelectorExpr)
					if ok && sel.Sel.Name == "Summary" {
						t.Errorf("%s: direct .Summary assignment — use AppendSummary/WithSummary",
							fset.Position(sel.Pos()))
					}
				}
			}
			return true
		})
	}
}

func TestEngineProseFloor(t *testing.T) {
	if n := len(engineProseKeys(t)); n < 150 {
		t.Fatalf("collected only %d engine-prose literals — the scan has gone blind (helpers renamed? dir moved?)", n)
	}
}

// TestEngineProseNoDynamic is the strict-literal gate: after the migration,
// every localizable format/step in internal/engine must ORIGINATE as a
// literal at its helper call site (WithSummary/AppendSummary arg 0,
// Progressf args 0/1, PromptReq arg 1) or as a Progress{Step: "..."}
// literal — a dynamic value there can't be a catalog key. This is the
// erroring half that engineProseKeys (the pure collector) deliberately does
// NOT do, split out so the collector stays green during the migration waves
// and this arms only once all ops are converted. Skips msg.go (the helper
// definitions, where Progressf/PromptReq forward caller parameters).
func TestEngineProseNoDynamic(t *testing.T) {
	fset := token.NewFileSet()
	ents, err := os.ReadDir(engineDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		name := e.Name()
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
				var litArgs []int
				switch calleeName(node) {
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
					if i < len(node.Args) {
						if _, ok := stringLit(node.Args[i]); !ok {
							t.Errorf("%s: %s arg %d must be a string literal (restructure branches; dynamic text is an ARG)",
								fset.Position(node.Pos()), calleeName(node), i)
						}
					}
				}
			case *ast.CompositeLit:
				id, ok := node.Type.(*ast.Ident)
				if !ok || id.Name != "Progress" {
					return true
				}
				for _, el := range node.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Step" {
						if _, ok := stringLit(kv.Value); !ok {
							t.Errorf("%s: Progress.Step must be a string literal", fset.Position(kv.Pos()))
						}
					}
				}
			}
			return true
		})
	}
}
