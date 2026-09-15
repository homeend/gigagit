package cli

import (
	"encoding/json"
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
	// rm/rename parse flags like every other sub-verb: a typo'd flag is a usage
	// error, not a lookup for a preview literally named "--foo".
	if code, _, errb := runCLI(t, dir, "preview", "rm", "--foo"); code != 2 || !strings.Contains(errb, "foo") {
		t.Fatalf("rm unknown flag: %d %q", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "preview", "rename", "--foo", "x"); code != 2 || !strings.Contains(errb, "foo") {
		t.Fatalf("rename unknown flag: %d %q", code, errb)
	}
}

// gg preview diff --hunks accepts an id, a label, or the raw <source>
// <target> pair, and all three must number the exact same patch `gg diff
// --preview <id> --hunks` prints (ruling 1: one construction of the patch).
// previewRepo sets XDG_STATE_HOME/HOME via t.Setenv, so this stays serial.
func TestPreviewDiffHunksAddressingForms(t *testing.T) {
	dir := previewRepo(t)
	code, id, errb := runCLI(t, dir, "preview", "add", "--label", "login", "feat/x", "main")
	if code != 0 {
		t.Fatalf("add: %d %s", code, errb)
	}
	id = strings.TrimSpace(id)

	code, want, errb := runCLI(t, dir, "diff", "--preview", id, "--hunks")
	if code != 0 {
		t.Fatalf("gg diff --preview: exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(want, "a.txt") || !strings.Contains(want, "  1 @@") {
		t.Fatalf("want numbered hunks over a.txt, got %q", want)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"id", []string{"preview", "diff", "--hunks", id}},
		{"label", []string{"preview", "diff", "--hunks", "login"}},
		{"pair", []string{"preview", "diff", "--hunks", "feat/x", "main"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, errb := runCLI(t, dir, c.args...)
			if code != 0 {
				t.Fatalf("exit=%d stderr=%s", code, errb)
			}
			if out != want {
				t.Fatalf("hunks diverged from `gg diff --preview --hunks`:\n%s\nvs\n%s", out, want)
			}
		})
	}
}

func TestPreviewDiffHunksJSON(t *testing.T) {
	dir := previewRepo(t)
	code, out, errb := runCLI(t, dir, "preview", "diff", "--hunks", "--json", "feat/x", "main")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var got []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not the documented JSON array: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0].Path != "a.txt" {
		t.Fatalf("json = %+v, want a.txt", got)
	}
}

// The two refusals are pure flag validation ahead of any git/store access, so
// a minimal, env-isolation-free repo is enough — these run in parallel.
func TestPreviewDiffHunksJSONRequiresHunks(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "preview", "diff", "--json")
	if code != 2 || !strings.Contains(errb, "requires") {
		t.Fatalf("want exit 2 'requires', got %d %q", code, errb)
	}
}

func TestPreviewDiffHunksPatchMutuallyExclusive(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "preview", "diff", "--hunks", "--patch")
	if code != 2 || !strings.Contains(errb, "mutually exclusive") {
		t.Fatalf("want exit 2 'mutually exclusive', got %d %q", code, errb)
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
