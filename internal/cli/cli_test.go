package cli

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, workdir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(workdir, args, strings.NewReader(""), &out, &errb, "")
	return code, out.String(), errb.String()
}

func TestStatusCommand(t *testing.T) {
	dir := newRepoDir(t)
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "main") {
		t.Fatalf("status output missing branch:\n%s", out)
	}
}

func TestCommitCommand(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	code, _, errb := runCLI(t, dir, "commit", "-m", "second", "--all")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 || !strings.Contains(out, "clean") {
		t.Fatalf("expected clean status after commit, got:\n%s", out)
	}
}

func TestCommitShortAllAlias(t *testing.T) {
	dir := newRepoDir(t)
	// modify a tracked file so -a has something to stage
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errb := runCLI(t, dir, "commit", "-m", "via alias", "-a")
	if code != 0 {
		t.Fatalf("commit -a exit = %d, stderr=%s", code, errb)
	}
}

func TestCommitRequiresMessage(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "commit")
	if code == 0 {
		t.Fatal("commit without -m should fail")
	}
}

func TestUnknownCommand(t *testing.T) {
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "frobnicate")
	if code == 0 {
		t.Fatal("unknown command should return non-zero")
	}
	if !strings.Contains(errb, "unknown") {
		t.Fatalf("expected 'unknown' in stderr:\n%s", errb)
	}
}

func TestNoArgsReturnsUsage(t *testing.T) {
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir) // no subcommand
	if code == 0 {
		t.Fatal("no command should return non-zero")
	}
	if !strings.Contains(errb, "usage") {
		t.Fatalf("expected usage on stderr:\n%s", errb)
	}
}

// TestEverySwitchCaseIsRegistered guards against the recurring drift where a
// subcommand gains a `case "x":` arm in Run but is forgotten in the commands
// map — so the real gg binary (which gates CLI vs TUI on IsCommand) prints
// "unknown command" while in-process tests, which call Run directly, never
// notice. It parses Run's switch and asserts every case string is a command.
func TestEverySwitchCaseIsRegistered(t *testing.T) {
	for _, c := range runSwitchCases(t) {
		if !IsCommand(c) {
			t.Errorf("Run handles case %q but it is missing from the commands map "+
				"(IsCommand returns false → the real gg binary will say %q is unknown)", c, c)
		}
	}
}

// runSwitchCases parses cli.go and returns the string literals of every `case`
// arm in Run's top-level `switch cmd` statement.
func runSwitchCases(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "cli.go", nil, 0)
	if err != nil {
		t.Fatalf("parse cli.go: %v", err)
	}
	var run *ast.FuncDecl
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "Run" {
			run = fn
			break
		}
	}
	if run == nil {
		t.Fatal("could not find func Run in cli.go")
	}
	var cases []string
	ast.Inspect(run, func(n ast.Node) bool {
		cc, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expr := range cc.List { // nil List = the default arm
			if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				cases = append(cases, strings.Trim(lit.Value, `"`))
			}
		}
		return true
	})
	if len(cases) == 0 {
		t.Fatal("no switch cases found in Run — parser change?")
	}
	return cases
}

func TestIsCommand(t *testing.T) {
	for _, c := range []string{"status", "commit", "pull", "push", "switch", "stash", "undo", "inspect"} {
		if !IsCommand(c) {
			t.Fatalf("%q should be a known command", c)
		}
	}
	if IsCommand("definitely-not-a-command") {
		t.Fatal("unknown token must not be a command")
	}
}
