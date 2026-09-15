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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

const preflightDir = "../preflight"

// domainDir is scanned alongside preflightDir because a Feature's Migrate
// lives in internal/domain (features.go): its Describe returns a
// preflight.Text built there, so the consent prose the spec's Prose section
// names is a SelectorExpr composite literal in this directory, not a bare
// Ident one in internal/preflight. Scanning only internal/preflight would
// leave the gate blind to exactly the string it exists to check.
const domainDir = "../domain"

// collectPreflightProseKeys walks one parsed file and adds the Format field of
// every Text{...} / preflight.Text{...} composite literal to keys. Both
// spellings are matched: the type is a bare Ident inside internal/preflight
// and a SelectorExpr everywhere else. Split out of preflightProseKeys so the
// matcher itself is directly testable against a synthetic source — otherwise
// widening the scan is unfalsifiable while domain declares no Migrate yet
// (see TestPreflightProseScanMatchesQualifiedText).
func collectPreflightProseKeys(t *testing.T, fset *token.FileSet, f *ast.File, keys map[string]bool) {
	t.Helper()
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if !isPreflightTextType(cl.Type) {
			return true
		}
		for _, el := range cl.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			k, ok := kv.Key.(*ast.Ident)
			if !ok || k.Name != "Format" {
				continue
			}
			if s, ok := stringLit(kv.Value); ok {
				keys[s] = true
			} else {
				t.Errorf("%s: Text.Format must be a string literal", fset.Position(kv.Pos()))
			}
		}
		return true
	})
}

// isPreflightTextType reports whether a composite literal's type names
// preflight.Text — either unqualified (inside internal/preflight) or through
// a package selector (everywhere else).
func isPreflightTextType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name == "Text"
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		return ok && pkg.Name == "preflight" && t.Sel.Name == "Text"
	}
	return false
}

// preflightProseKeys parses every non-test .go file in internal/preflight AND
// internal/domain and returns the set of localizable literals: the Format
// field of every Text{Format: "...", ...} composite literal (the shape a
// Requirement's Reason method and a Migrate's Describe return). Mirrors
// engineProseKeys, and is a pure collector for the same reason: a non-literal
// Format can't be a catalog key, and both scanned packages have a closed set
// of prose sites, so there is no migration wave to gate incrementally — every
// Format here is expected to already be a literal.
func preflightProseKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range []string{preflightDir, domainDir} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if perr != nil {
				t.Fatalf("parse %s: %v", name, perr)
			}
			collectPreflightProseKeys(t, fset, f, keys)
		}
	}
	return keys
}

// TestPreflightProseScanMatchesQualifiedText proves the widened matcher
// actually sees a qualified preflight.Text{...} — the shape a real
// domain.Features() Migrate.Describe will use. Without this, the widening is
// unfalsifiable: today's domain declares no Migrate, so scanning ../domain
// contributes zero keys and TestPreflightProseKeysInBundles would keep
// passing even if the matcher never matched anything there.
func TestPreflightProseScanMatchesQualifiedText(t *testing.T) {
	t.Parallel()
	const src = `package domain

import "github.com/homeend/gigagit/internal/preflight"

var m = preflight.Migrate{
	Describe: func() preflight.Text {
		return preflight.Text{Format: "this discards every recorded version of %s", Args: []any{"main"}}
	},
}

var bare = Text{Format: "unqualified still matches"}

var other = pkg.Text{Format: "a different package's Text must NOT match"}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "synthetic.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	collectPreflightProseKeys(t, fset, f, keys)

	if !keys["this discards every recorded version of %s"] {
		t.Errorf("qualified preflight.Text{...} was not collected: %v", keys)
	}
	if !keys["unqualified still matches"] {
		t.Errorf("bare Text{...} was not collected: %v", keys)
	}
	if keys["a different package's Text must NOT match"] {
		t.Errorf("a Text from another package must not be collected: %v", keys)
	}
}

// TestPreflightProseScanRejectsNonLiteralFormat proves the collector still
// FAILS a Format that cannot be a catalog key, rather than silently skipping
// it. Run against a throwaway *testing.T so the expected failure is observed
// instead of failing this test.
func TestPreflightProseScanRejectsNonLiteralFormat(t *testing.T) {
	t.Parallel()
	const src = `package domain

var v = preflight.Text{Format: someVar}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "synthetic.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	sub := &testing.T{}
	collectPreflightProseKeys(sub, fset, f, map[string]bool{})
	if !sub.Failed() {
		t.Error("a non-literal Text.Format must be reported, not skipped")
	}
}

func TestPreflightProseKeysInBundles(t *testing.T) {
	t.Parallel()
	keys := preflightProseKeys(t)
	if len(keys) == 0 {
		t.Fatal("collected 0 preflight-prose literals — the scan has gone blind (Text moved/renamed?)")
	}
	builtins := i18n.Builtins()
	for _, code := range []string{"ja", "ko", "zh", "ru"} {
		b, ok := builtins[code]
		if !ok {
			t.Fatalf("embedded bundle %s missing", code)
		}
		for k := range keys {
			if _, has := b[k]; !has {
				t.Errorf("%s.toml: missing preflight-prose key %q", code, k)
			}
		}
	}
}
