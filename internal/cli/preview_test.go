package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func previewRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := newCLIRepo(t)
	gitRun(t, dir, "checkout", "-q", "-b", "feat/x")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "add a")
	gitRun(t, dir, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(dir, "m.txt"), []byte("m\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "main moves")
	return dir
}

func TestPreviewAddListShow(t *testing.T) {
	dir := previewRepo(t)
	code, id, errb := runCLI(t, dir, "preview", "add", "--label", "login", "feat/x", "main")
	if code != 0 {
		t.Fatalf("add: %d %s", code, errb)
	}
	id = strings.TrimSpace(id)
	_, out, _ := runCLI(t, dir, "preview", "list")
	if !strings.Contains(out, id+"\tlogin\tfeat/x\tmain\tok\t1\t1") {
		t.Fatalf("list = %q", out)
	}
	_, out, _ = runCLI(t, dir, "preview", "show", "login")
	if !strings.Contains(out, "A\ta.txt") || strings.Contains(out, "m.txt") {
		t.Fatalf("show must list only what feat/x brings: %q", out)
	}
	_, out, _ = runCLI(t, dir, "preview", "show", "--patch", id)
	if !strings.Contains(out, "+a") {
		t.Fatalf("patch = %q", out)
	}
	if code, _, errb := runCLI(t, dir, "preview", "add", "feat/x", "main"); code != 1 || !strings.Contains(errb, id) {
		t.Fatalf("duplicate add: %d %q (must name the existing id)", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "preview", "rename", id, "renamed"); code != 0 {
		t.Fatal("rename")
	}
	if code, _, _ := runCLI(t, dir, "preview", "rm", "renamed"); code != 0 {
		t.Fatal("rm by label")
	}
	if _, out, _ := runCLI(t, dir, "preview", "list"); strings.TrimSpace(out) != "" {
		t.Fatalf("list after rm = %q", out)
	}
}

func TestPreviewDiffOnceAndMergedState(t *testing.T) {
	dir := previewRepo(t)
	if code, out, _ := runCLI(t, dir, "preview", "diff", "feat/x", "main"); code != 0 || !strings.Contains(out, "a.txt") {
		t.Fatalf("diff: %d %q", code, out)
	}
	runCLI(t, dir, "preview", "add", "feat/x", "main")
	gitRun(t, dir, "merge", "-q", "--no-edit", "feat/x")
	code, out, errb := runCLI(t, dir, "preview", "show", "feat/x → main")
	if code != 1 || out != "" || !strings.Contains(errb, "merged") {
		t.Fatalf("show merged: %d %q %q", code, out, errb)
	}
	if _, out, _ := runCLI(t, dir, "preview", "list"); !strings.Contains(out, "\tmerged\t0\t0") {
		t.Fatalf("list merged = %q", out)
	}
}

func TestPreviewUsageErrors(t *testing.T) {
	dir := previewRepo(t)
	if code, _, _ := runCLI(t, dir, "preview"); code != 2 {
		t.Fatal("no subcommand → 2")
	}
	if code, _, errb := runCLI(t, dir, "preview", "add", "nope", "main"); code != 1 || !strings.Contains(errb, "nope") {
		t.Fatalf("unknown branch: %d %q", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "preview", "show", "missing"); code != 1 {
		t.Fatal("unknown id → 1")
	}
}

func TestPreviewListUsageErrors(t *testing.T) {
	dir := previewRepo(t)
	if code, _, errb := runCLI(t, dir, "preview", "list", "--bogus"); code != 2 || !strings.Contains(errb, "bogus") {
		t.Fatalf("unknown flag: %d %q", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "preview", "list", "extra"); code != 2 {
		t.Fatal("extra positional arg → 2")
	}
}
