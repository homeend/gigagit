package domain

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// stateBaseDirKinds is every kind string this package is allowed to ask
// stateBaseDir for, and each one is a DIRECTORY NAME ON A REAL USER'S DISK.
// Changing one does not break anything a test can see — it silently orphans
// that store's existing data, because every test sets XDG_STATE_HOME to a
// temp dir and then writes AND reads through the same (wrong) path, so the
// bug is self-consistent. Adding a row here is a deliberate act; editing one
// is a data migration.
var stateBaseDirKinds = []string{
	"bookmark",
	"linkhist",
	"notes",
	"prefix",
	"previews",
	"profile",
	"reviews",
	"search",
	"shelf",
	"tasks",
}

// TestStateBaseDirCallersPassKnownKinds walks this package's own source and
// pins the exact SET of literals handed to stateBaseDir.
//
// TestStateBaseDirKinds (statebasedir_test.go) proves stateBaseDir maps a
// kind to the right path. It cannot prove each store passes the right kind:
// with shelfstore.go changed to stateBaseDir("shelves"), that test — and all
// 493 others in this package — still passed. This is the test that fails.
//
// It is deliberately a source-level assertion. The runtime alternative would
// have to construct nine stores against a real repo, and it would still only
// check the kinds it thought to enumerate; reading the calls catches a NEW
// store that quietly reuses an existing kind too, which would make two
// stores share one directory.
func TestStateBaseDirCallersPassKnownKinds(t *testing.T) {
	t.Parallel()
	allowed := make(map[string]bool, len(stateBaseDirKinds))
	for _, k := range stateBaseDirKinds {
		allowed[k] = true
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := map[string]string{} // kind -> the file that asks for it
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "stateBaseDir" || len(call.Args) != 1 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s: stateBaseDir is called with a non-literal argument — "+
					"the kind must be a constant so this gate can see it",
					fset.Position(call.Pos()))
				return true
			}
			kind, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Errorf("%s: unquote %s: %v", fset.Position(call.Pos()), lit.Value, err)
				return true
			}
			if !allowed[kind] {
				t.Errorf("%s: stateBaseDir(%q) — %q is not a known state kind. "+
					"If this renames an existing store's directory it ORPHANS every "+
					"user's data there; if it is a new store, add it to "+
					"stateBaseDirKinds (and to TestStateBaseDirKinds).",
					fset.Position(call.Pos()), kind, kind)
			}
			if prev, dup := seen[kind]; dup {
				t.Errorf("%s: stateBaseDir(%q) duplicates %s — two stores would "+
					"share one directory", fset.Position(call.Pos()), kind, prev)
			}
			seen[kind] = filepath.Base(fset.Position(call.Pos()).Filename)
			return true
		})
	}

	// Every declared kind must actually be reached, or the list has grown a
	// row for a store that no longer exists and the gate is guarding nothing.
	var missing []string
	for _, k := range stateBaseDirKinds {
		if seen[k] == "" {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no caller asks for %v — remove the row, or restore the caller", missing)
	}
}
