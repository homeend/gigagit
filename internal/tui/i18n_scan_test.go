package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/i18n"
)

// tuiI18nCatalog parses every non-test .go file in this package and returns
// the set of i18n.T keys. The gitconfdocs staleness pattern applied to code:
// the source itself is the catalog of record.
func tuiI18nCatalog(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	keys := map[string]bool{}
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		// Forbid aliased imports of the i18n package — the T-call scan matches
		// the bare identifier "i18n", so aliasing would make calls invisible.
		for _, imp := range f.Imports {
			if imp.Path.Value == `"github.com/homeend/gigagit/internal/i18n"` && imp.Name != nil {
				t.Errorf("%s: the i18n package must be imported without an alias (the T-call scan matches the bare identifier)", name)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "T" {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "i18n" {
				return true
			}
			if len(call.Args) == 0 {
				t.Errorf("%s: i18n.T with no arguments", fset.Position(call.Pos()))
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				// Dynamic keys can't be extracted or translated — dynamic
				// text must be a T *argument*, never part of the key. The
				// ONE sanctioned exception: i18n_engine.go renders engine
				// (format, args) pairs whose formats are gate-checked
				// against all four bundles by engine_prose_test.go instead.
				if name == "i18n_engine.go" {
					return true
				}
				t.Errorf("%s: i18n.T key must be a string literal", fset.Position(call.Pos()))
				return true
			}
			s, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				t.Errorf("%s: unquote: %v", fset.Position(call.Pos()), uerr)
				return true
			}
			keys[s] = true
			return true
		})
	}
	return keys
}

func TestI18nKeysAreLiterals(t *testing.T) {
	t.Parallel()
	_ = tuiI18nCatalog(t) // the walk itself reports non-literal keys
}

func TestI18nBundlesComplete(t *testing.T) {
	t.Parallel()
	catalog := tuiI18nCatalog(t)
	for k := range engineProseKeys(t) { // engine formats are used keys too
		catalog[k] = true
	}
	builtins := i18n.Builtins()
	for _, code := range []string{"ja", "ko", "zh", "ru"} {
		b, ok := builtins[code]
		if !ok {
			t.Fatalf("embedded bundle %s missing", code)
		}
		for key := range b {
			if !catalog[key] {
				t.Errorf("%s.toml: orphaned key %q — no i18n.T call site uses it (English text changed?)", code, key)
			}
		}
		for key := range catalog {
			if _, has := b[key]; !has {
				t.Errorf("%s.toml: missing translation for %q", code, key)
			}
		}
		for key, tr := range b {
			if err := i18n.CheckVerbs(key, tr); err != nil {
				t.Errorf("%s.toml %q: %v", code, key, err)
			}
		}
	}
}
