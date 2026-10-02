package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateAddListShowRenderRemove(t *testing.T) {
	dir := prefixRepo(t)

	code, out, errb := runCLIStdin(t, dir, "Hi <user:name>,\n<br> on <branch>\n", "template", "add", "--title", "Greeting", "-F", "-")
	if code != 0 || strings.TrimSpace(out) != "greeting" {
		t.Fatalf("add exit %d out %q err %s", code, out, errb)
	}
	if _, out, _ = runCLI(t, dir, "template", "list"); !strings.Contains(out, "greeting\trepo\tGreeting") {
		t.Fatalf("list = %q", out)
	}
	if _, out, _ = runCLI(t, dir, "templates"); !strings.Contains(out, "greeting") {
		t.Fatalf("bare templates = %q", out)
	}
	if _, out, _ = runCLI(t, dir, "template", "show", "greet"); !strings.Contains(out, "Hi <user:name>,") || !strings.Contains(out, "variables: name") || !strings.Contains(out, "automatic: <branch>") {
		t.Fatalf("show = %q", out)
	}
	code, _, errb = runCLI(t, dir, "template", "render", "greeting")
	if code != 2 || !strings.Contains(errb, "--set name=") {
		t.Fatalf("render without --set: exit %d err %q", code, errb)
	}
	code, out, errb = runCLI(t, dir, "template", "render", "greeting", "--set", "name=Ann")
	if code != 0 || !strings.HasPrefix(out, "Hi Ann,\n<br> on ") || !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Fatalf("render exit %d out %q err %s", code, out, errb)
	}
	if code, _, _ = runCLI(t, dir, "template", "rm", "greeting"); code != 0 {
		t.Fatalf("rm exit %d", code)
	}
	if _, out, _ = runCLI(t, dir, "template", "list"); strings.Contains(out, "greeting") {
		t.Fatalf("still listed: %q", out)
	}
}

func TestTemplateEditAndFile(t *testing.T) {
	dir := prefixRepo(t)
	f := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(f, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errb := runCLI(t, dir, "template", "add", "--global", "--title", "Note", "-F", f); code != 0 {
		t.Fatalf("add: %s", errb)
	}
	if _, out, _ := runCLI(t, dir, "template", "list"); !strings.Contains(out, "note\tglobal\tNote") {
		t.Fatalf("list = %q", out)
	}
	if code, _, errb := runCLIStdin(t, dir, "two\n", "template", "edit", "note", "--global", "-F", "-"); code != 0 {
		t.Fatalf("edit body: %s", errb)
	}
	code, out, errb := runCLI(t, dir, "template", "edit", "note", "--title", "Memo")
	if code != 0 || strings.TrimSpace(out) != "memo" {
		t.Fatalf("rename exit %d out %q err %s", code, out, errb)
	}
	if _, out, _ = runCLI(t, dir, "template", "render", "memo"); out != "two\n" {
		t.Fatalf("render = %q", out)
	}
}

func TestTemplateSeqPeekAndTake(t *testing.T) {
	dir := prefixRepo(t)
	runCLIStdin(t, dir, "#<seq:cli:2>", "template", "add", "--title", "Seq", "-F", "-")
	for i := 0; i < 2; i++ {
		if _, out, _ := runCLI(t, dir, "template", "render", "seq", "--peek"); out != "#01\n" {
			t.Fatalf("peek %d = %q", i, out)
		}
	}
	if _, out, _ := runCLI(t, dir, "template", "render", "seq"); out != "#01\n" {
		t.Fatalf("take = %q", out)
	}
	if _, out, _ := runCLI(t, dir, "template", "render", "seq", "--peek"); out != "#02\n" {
		t.Fatalf("after take = %q", out)
	}
}

func TestTemplateUsageErrors(t *testing.T) {
	dir := prefixRepo(t)
	for _, args := range [][]string{
		{"template"}, {"template", "nope"}, {"template", "add"}, {"template", "add", "--title", "x"},
		{"template", "show"}, {"template", "render", "missing"}, {"template", "edit", "x"},
		{"template", "rm"}, {"template", "rm", "missing"},
	} {
		if code, _, _ := runCLI(t, dir, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if code, _, errb := runCLIStdin(t, dir, "x <seq> y", "template", "add", "--title", "Bad", "-F", "-"); code != 1 || !strings.Contains(errb, "seq") {
		t.Fatalf("malformed token: exit %d err %q", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "template", "add", "--title", "Gone", "-F", filepath.Join(t.TempDir(), "absent.md")); code != 1 {
		t.Fatalf("unreadable body file: exit %d, want 1", code)
	}
}

// A relative -F path is taken from the directory the command runs against.
func TestTemplateAddRelativeFile(t *testing.T) {
	dir := prefixRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "body.md"), []byte("rel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errb := runCLI(t, dir, "template", "add", "--title", "Rel", "-F", "body.md"); code != 0 {
		t.Fatalf("add: %s", errb)
	}
	if _, out, _ := runCLI(t, dir, "template", "render", "rel"); out != "rel\n" {
		t.Fatalf("render = %q", out)
	}
}
