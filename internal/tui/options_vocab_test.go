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
//
// When the `Options:` VALUE ITSELF is a bare identifier (a variable-built
// slice, e.g. `Options: scopeOpts`), it is resolved within its ENCLOSING
// FUNCTION: every `ident := []string{…}` / `ident = []string{…}` /
// `var ident = []string{…}` assignment to that name anywhere in the function
// body is unioned together (an if/else that assigns different option sets to
// the same variable must have both sets covered), with elements resolved
// against the same package-level consts plus any function-local `const`
// declarations. If the identifier can't be resolved to a static []string this
// way, it is a hard failure (t.Errorf), not a silent Logf — a variable-built
// Options slice must still be statically translatable.
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

		// Every top-level function/method declaration with a body, across all
		// files in the package — the granularity "enclosing function" resolves
		// against.
		var funcDecls []*ast.FuncDecl
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
					funcDecls = append(funcDecls, fd)
				}
			}
		}

		// Pass 1: function-local `const` declarations (e.g. checkout_as_popup's
		// `const rename = "check out as different name…"`), keyed per function
		// since two functions may reuse the same local const name.
		funcConsts := map[*ast.FuncDecl]map[string]string{}
		for _, fd := range funcDecls {
			lc := map[string]string{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				gd, ok := n.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					return true
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
									lc[name.Name] = v
								}
							}
						}
					}
				}
				return true
			})
			funcConsts[fd] = lc
		}

		// resolveElem resolves one composite-literal element against the
		// function-local consts first, then the package-level consts.
		resolveElem := func(fd *ast.FuncDecl, el ast.Expr) (string, bool) {
			switch e := el.(type) {
			case *ast.BasicLit:
				if e.Kind == token.STRING {
					if v, err := strconv.Unquote(e.Value); err == nil {
						return v, true
					}
				}
			case *ast.Ident:
				if v, ok := funcConsts[fd][e.Name]; ok {
					return v, true
				}
				if v, ok := consts[e.Name]; ok {
					return v, true
				}
			}
			return "", false
		}

		// sliceInfo accumulates every value ever assigned to one identifier
		// within one function; ok is false the moment any assignment contains
		// an element that can't be resolved statically (poisoned — a variable
		// this dynamic can't be trusted for any of its assignments).
		type sliceInfo struct {
			values []string
			ok     bool
		}

		// Pass 2: local `[]string` slice assignments (:=, =, and `var … = …`)
		// anywhere in each function's body, unioned per identifier name.
		funcSlices := map[*ast.FuncDecl]map[string]*sliceInfo{}
		for _, fd := range funcDecls {
			sl := map[string]*sliceInfo{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				var name string
				var rhs ast.Expr
				switch stmt := n.(type) {
				case *ast.AssignStmt:
					if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
						return true
					}
					id, ok := stmt.Lhs[0].(*ast.Ident)
					if !ok {
						return true
					}
					name, rhs = id.Name, stmt.Rhs[0]
				case *ast.ValueSpec:
					if len(stmt.Names) != 1 || len(stmt.Values) != 1 {
						return true
					}
					name, rhs = stmt.Names[0].Name, stmt.Values[0]
				default:
					return true
				}
				cl, ok := rhs.(*ast.CompositeLit)
				if !ok {
					return true
				}
				if at, ok := cl.Type.(*ast.ArrayType); ok {
					if id, ok := at.Elt.(*ast.Ident); !ok || id.Name != "string" {
						return true // some other []T literal — not an Options slice
					}
				}
				info := sl[name]
				if info == nil {
					info = &sliceInfo{ok: true}
					sl[name] = info
				}
				for _, el := range cl.Elts {
					if v, resolved := resolveElem(fd, el); resolved {
						info.values = append(info.values, v)
					} else {
						info.ok = false
					}
				}
				return true
			})
			funcSlices[fd] = sl
		}

		enclosingFunc := func(pos token.Pos) *ast.FuncDecl {
			for _, fd := range funcDecls {
				if fd.Body.Pos() <= pos && pos <= fd.Body.End() {
					return fd
				}
			}
			return nil
		}

		// isParam reports whether name is a parameter of fd. A pass-through
		// helper (engine.PromptReq forwards its `options` parameter to
		// DecisionRequest.Options) cannot know its option values statically;
		// they arrive at the call sites, collected separately below.
		isParam := func(fd *ast.FuncDecl, name string) bool {
			if fd == nil || fd.Type.Params == nil {
				return false
			}
			for _, field := range fd.Type.Params.List {
				for _, id := range field.Names {
					if id.Name == name {
						return true
					}
				}
			}
			return false
		}

		// resolveOptions collects one options expression's string values into
		// out — a `[]string{…}` literal or a variable-built slice identifier.
		// Shared by the `Options:` struct field and engine.PromptReq's
		// call-site options argument, so migrating a decision from a bare
		// DecisionRequest literal to PromptReq keeps its options covered.
		resolveOptions := func(fd *ast.FuncDecl, value ast.Expr) {
			switch v := value.(type) {
			case *ast.CompositeLit:
				for _, el := range v.Elts {
					pos := fset.Position(el.Pos()).String()
					switch e := el.(type) {
					case *ast.BasicLit:
						if e.Kind == token.STRING {
							if val, err := strconv.Unquote(e.Value); err == nil {
								out[val] = pos
							}
						}
					case *ast.Ident:
						if val, ok := consts[e.Name]; ok {
							out[val] = pos
						} else {
							t.Errorf("%s: option element %s is not a same-package string const — use a literal or add resolution", pos, e.Name)
						}
					default:
						t.Errorf("%s: option element is neither a string literal nor a const identifier", pos)
					}
				}
			case *ast.Ident:
				pos := fset.Position(value.Pos()).String()
				if isParam(fd, v.Name) {
					return // pass-through helper param; values come from call sites
				}
				var info *sliceInfo
				if fd != nil {
					info = funcSlices[fd][v.Name]
				}
				if info == nil || !info.ok {
					t.Errorf("%s: option identifier %s could not be resolved to a static []string in its enclosing function — add a resolvable local slice assignment or a literal", pos, v.Name)
					return
				}
				for _, val := range info.values {
					out[val] = pos
				}
			default:
				// Neither a composite literal nor a plain identifier (e.g. an
				// append(...) call building option names dynamically) — keep it
				// visible, never silent.
				t.Logf("%s: options value is not a composite literal — dynamic values render untranslated by design", fset.Position(value.Pos()))
			}
		}

		for _, f := range pkg.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.KeyValueExpr:
					key, ok := node.Key.(*ast.Ident)
					if !ok || key.Name != "Options" {
						return true
					}
					resolveOptions(enclosingFunc(node.Pos()), node.Value)
				case *ast.CallExpr:
					// engine.PromptReq(id, format, options, args...) — arg 2 is
					// the options slice, now that prompts build through the helper.
					if calleeName(node) == "PromptReq" && len(node.Args) >= 3 {
						resolveOptions(enclosingFunc(node.Pos()), node.Args[2])
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
	t.Parallel()
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
