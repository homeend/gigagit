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

// collectOptionValues walks a package directory and returns every element of
// every `Options: []string{…}` composite literal in non-test sources. String
// literals are taken directly; identifiers are resolved through the package's
// own top-level string constants (e.g. engine's applyOptWorkingTree). An
// element it cannot resolve statically is reported — dynamic option values
// belong in a variable-built slice, not a literal.
func collectOptionValues(t *testing.T, dir string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	out := map[string]string{}
	for _, pkg := range pkgs {
		consts := map[string]string{}
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if i < len(vs.Values) {
							if bl, ok := vs.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
								if v, err := strconv.Unquote(bl.Value); err == nil {
									consts[name.Name] = v
								}
							}
						}
					}
				}
			}
		}
		for _, f := range pkg.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				kv, ok := n.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || key.Name != "Options" {
					return true
				}
				cl, ok := kv.Value.(*ast.CompositeLit)
				if !ok {
					// A dynamic slice is legitimate (values are names; passthrough
					// renders them correctly) — but keep it visible, never silent.
					t.Logf("%s: Options is not a composite literal — dynamic values render untranslated by design", fset.Position(kv.Pos()))
					return true
				}
				for _, el := range cl.Elts {
					pos := fset.Position(el.Pos()).String()
					switch e := el.(type) {
					case *ast.BasicLit:
						if e.Kind == token.STRING {
							if v, err := strconv.Unquote(e.Value); err == nil {
								out[v] = pos
							}
						}
					case *ast.Ident:
						if v, ok := consts[e.Name]; ok {
							out[v] = pos
						} else {
							t.Errorf("%s: option element %s is not a same-package string const — use a literal or add resolution", pos, e.Name)
						}
					default:
						t.Errorf("%s: option element is neither a string literal nor a const identifier", pos)
					}
				}
				return true
			})
		}
	}
	return out
}

// optionDisplayCaseKeys returns the set of i18n.T literal keys inside
// optionDisplayName's body — its case arms.
func optionDisplayCaseKeys(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "i18n_display.go", nil, 0)
	if err != nil {
		t.Fatalf("parse i18n_display.go: %v", err)
	}
	keys := map[string]bool{}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "optionDisplayName" {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "T" {
				return true
			}
			if bl, ok := call.Args[0].(*ast.BasicLit); ok && bl.Kind == token.STRING {
				if v, err := strconv.Unquote(bl.Value); err == nil {
					keys[v] = true
				}
			}
			return true
		})
	}
	if len(keys) == 0 {
		t.Fatal("optionDisplayName not found or has no T() cases")
	}
	return keys
}

// TestDecisionOptionValuesTranslated: every statically declared decision
// option value across engine+tui must (a) exist as a key in all four
// bundles and (b) have an optionDisplayName case, so the modal renders it
// translated. Dynamic values (names) legitimately pass through.
func TestDecisionOptionValuesTranslated(t *testing.T) {
	values := map[string]string{}
	for _, dir := range []string{".", "../engine"} {
		for v, pos := range collectOptionValues(t, dir) {
			values[v] = pos
		}
	}
	if len(values) < 30 {
		t.Fatalf("collected only %d option values — the scan is likely broken", len(values))
	}
	cases := optionDisplayCaseKeys(t)
	builtins := i18n.Builtins()
	for v, pos := range values {
		if !cases[v] {
			t.Errorf("option %q (%s): no optionDisplayName case — it would render untranslated", v, pos)
		}
		for _, code := range []string{"ja", "ko", "zh", "ru"} {
			if _, ok := builtins[code][v]; !ok {
				t.Errorf("option %q (%s): missing from %s.toml", v, pos, code)
			}
		}
	}
}
