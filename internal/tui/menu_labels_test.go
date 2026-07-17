package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/homeend/gigagit/internal/i18n"
)

// isI18nTCall reports whether call invokes the bare, unaliased i18n.T
// function — the same identifier shape i18n_scan_test.go's catalog scan
// matches (an aliased import would make this scan blind too, but that's
// already guarded there).
func isI18nTCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "T" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "i18n"
}

// i18nTKey extracts the string-literal key from an i18n.T(...) call, if its
// first argument is one (i18n_scan_test.go's TestI18nKeysAreLiterals
// separately enforces that it always must be — here we just skip silently
// if not, to avoid double-reporting the same defect two different ways).
func i18nTKey(call *ast.CallExpr) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// inspectLabelExpr walks expr for letter-bearing string literals that are
// NOT inside the arguments of a nested i18n.T(...) call. Any such literal is
// a violation, reported at its own position — a raw literal reaching a
// label (directly, or nested inside some other same-package helper call the
// label argument happens to be) with no translation. i18n.T is recognized
// however deeply it's nested (e.g. inside another function's call
// arguments), so an already-translated value threaded through a helper
// still exempts. Every i18n.T key literal found anywhere in expr is
// recorded into keys (keyed by the i18n.T call site's position) for the
// bundle-completeness check below. foundLiteral reports whether ANY string
// literal — violating or exempt — was seen at all; an expression with none
// (a bare identifier, a field selector, a data-driven call with no literal
// argument) is genuinely dynamic and never violates by design.
func inspectLabelExpr(fset *token.FileSet, expr ast.Expr, keys map[string]string) (violations []string, foundLiteral bool) {
	ast.Inspect(expr, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isI18nTCall(call) {
			foundLiteral = true
			if key, ok := i18nTKey(call); ok {
				keys[key] = fset.Position(call.Pos()).String()
			}
			return false // exempt: don't descend into the T(...) call's own args
		}
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil && hasLetter(s) {
				foundLiteral = true
				violations = append(violations, fset.Position(lit.Pos()).String())
			}
		}
		return true
	})
	return violations, foundLiteral
}

// actionRowLabelValue returns the value expression of an actionRow
// composite literal's "label" field. Every actionRow literal in the package
// is currently field-keyed (verified: `grep -n 'actionRow{"'` finds none),
// but a positional literal (field order: id, key, label, copyText, run) is
// supported defensively too. id:/key:/copyText:/run: are never inspected.
func actionRowLabelValue(cl *ast.CompositeLit) (ast.Expr, bool) {
	for i, el := range cl.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if isIdent(kv.Key, "label") {
				return kv.Value, true
			}
			continue
		}
		if i == 2 { // positional fallback: id, key, label, ...
			return el, true
		}
	}
	return nil, false
}

// walkActionRowLits calls fn for every actionRow-typed composite literal in
// f. Two shapes appear in this package: the explicit `actionRow{...}` form,
// and elided-type elements inside a `[]actionRow{...}` slice literal (e.g.
// commit_scope.go's `[]actionRow{{id: "graph-widen", label: ...}, ...}`) —
// Go elides the per-element type there, so those inner composite literals
// have a nil Type and can only be recognized via their enclosing slice.
func walkActionRowLits(f *ast.File, fn func(cl *ast.CompositeLit)) {
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if isIdent(cl.Type, "actionRow") {
			fn(cl)
			return true
		}
		if at, ok := cl.Type.(*ast.ArrayType); ok && isIdent(at.Elt, "actionRow") {
			for _, el := range cl.Elts {
				if inner, ok := el.(*ast.CompositeLit); ok {
					fn(inner)
				}
			}
		}
		return true
	})
}

// funcParamLabelIndex returns the flat parameter index of a `label string`
// parameter in fd's signature, if any — flattening grouped parameter names
// (`func f(id, label string)` is two flat slots, both type string) the same
// way the real call-argument list is flattened.
func funcParamLabelIndex(fd *ast.FuncDecl) (int, bool) {
	if fd.Type.Params == nil {
		return 0, false
	}
	idx := 0
	for _, field := range fd.Type.Params.List {
		if len(field.Names) == 0 {
			idx++ // unnamed parameter — still occupies a slot
			continue
		}
		for _, n := range field.Names {
			if n.Name == "label" && isIdent(field.Type, "string") {
				return idx, true
			}
			idx++
		}
	}
	return 0, false
}

// callFuncName returns the identifier a call expression resolves to within
// this package: the bare name for a plain function call, or the selector
// name for a method call (`m.commitEditRow(...)`) — same-package
// name-based resolution with no type-checking, the same approach
// options_vocab_test.go uses for const/slice resolution. This is what makes
// the label-parameter check work for methods (FuncDecls with receivers),
// not just plain functions.
func callFuncName(call *ast.CallExpr) (string, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name, true
	case *ast.SelectorExpr:
		return fun.Sel.Name, true
	}
	return "", false
}

// TestActionMenuLabelsTranslated is the AST enforcement gate for the `.`
// action-menu row label: every action-row label in package tui must route
// through i18n.T, so Wave D's coverage can't regress as new menu rows are
// added. Two checks, modeled on options_vocab_test.go's ParseDir scaffolding
// and bundle-check approach:
//
//  1. Every actionRow composite literal's "label:" field value.
//  2. Every call-site argument at the parameter position of any
//     same-package function or method whose signature has a `label string`
//     parameter — resolved by indexing FuncDecls' param names. This catches
//     prose reaching a row POSITIONALLY through a helper with no `label:`
//     token in sight at the call site, e.g.
//     `commitEditRow(id, label string, …)` called as
//     `m.commitEditRow("commit-drop", "Drop commit", …)` — the exact blind
//     spot Task 7 proved real.
//
// For each inspected expression: any *ast.BasicLit STRING containing at
// least one Unicode letter is a violation (file:line via t.Errorf) UNLESS
// it sits inside the arguments of an i18n.T(...) call, however deeply
// nested. Letterless literals (separators, "%s ↔ %s"-style formatting) pass
// silently. An expression with no letter-bearing literal at all (a bare
// identifier, a data-driven call) is logged and passes — genuinely dynamic
// labels are allowed by design. Every i18n.T key found this way must exist
// in all four embedded bundles.
func TestActionMenuLabelsTranslated(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse .: %v", err)
	}

	keys := map[string]string{}
	checked := 0

	for _, pkg := range pkgs {
		// Index every FuncDecl (function or method, across all files) whose
		// signature has a `label string` parameter.
		labelParamIdx := map[string]int{}
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				if idx, ok := funcParamLabelIndex(fd); ok {
					labelParamIdx[fd.Name.Name] = idx
				}
			}
		}

		for _, f := range pkg.Files {
			// Check 1: actionRow composite literals' label: field.
			walkActionRowLits(f, func(cl *ast.CompositeLit) {
				val, ok := actionRowLabelValue(cl)
				if !ok {
					return
				}
				checked++
				violations, foundLiteral := inspectLabelExpr(fset, val, keys)
				for _, pos := range violations {
					t.Errorf("%s: actionRow label is a raw string literal — wrap it in i18n.T(...)", pos)
				}
				if !foundLiteral {
					t.Logf("%s: dynamic actionRow label (no literal) — allowed", fset.Position(val.Pos()).String())
				}
			})

			// Check 2: call-site arguments at a "label" parameter position.
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name, ok := callFuncName(call)
				if !ok {
					return true
				}
				idx, ok := labelParamIdx[name]
				if !ok || idx >= len(call.Args) {
					return true
				}
				checked++
				arg := call.Args[idx]
				violations, foundLiteral := inspectLabelExpr(fset, arg, keys)
				for _, pos := range violations {
					t.Errorf("%s: %s's label argument is a raw string literal — wrap it in i18n.T(...)", pos, name)
				}
				if !foundLiteral {
					t.Logf("%s: dynamic %s label argument (no literal) — allowed", fset.Position(arg.Pos()).String(), name)
				}
				return true
			})
		}
	}

	if checked < 30 {
		t.Fatalf("inspected only %d label expressions — the scan is likely broken", checked)
	}

	builtins := i18n.Builtins()
	for key, pos := range keys {
		for _, code := range []string{"ja", "ko", "zh", "ru"} {
			if _, ok := builtins[code][key]; !ok {
				t.Errorf("%s (%q): missing from %s.toml", pos, key, code)
			}
		}
	}
}
