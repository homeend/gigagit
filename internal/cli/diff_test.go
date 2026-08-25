package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestRenderStat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderStat(&buf, []model.DiffStat{
		{Path: "main.go", Added: 3, Deleted: 1},
		{Path: "img.png", Binary: true},
		{Path: "new.go", OldPath: "old.go", Added: 1},
	})
	want := "main.go +3 -1\nimg.png bin\nold.go => new.go +1 -0\n3 files +4 -1\n"
	if buf.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", buf.String(), want)
	}
}

func TestRenderStatEmpty(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderStat(&buf, nil)
	if buf.String() != "" {
		t.Fatalf("empty diff must print nothing, got %q", buf.String())
	}
}

func TestDiffStatWorkingTree(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nmore\nlines\n"), 0o644)
	code, out, errb := runCLI(t, dir, "diff", "--stat")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	want := "README.md +2 -0\n1 files +2 -0\n"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestDiffPatchDefault(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	code, out, _ := runCLI(t, dir, "diff")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "-hi") || !strings.Contains(out, "+changed") {
		t.Fatalf("patch missing hunks:\n%s", out)
	}
}

func TestDiffNameOnly(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	code, out, _ := runCLI(t, dir, "diff", "--name-only")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if out != "README.md\n" {
		t.Fatalf("got %q", out)
	}
}

func TestDiffEmptyPrintsNothing(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	for _, mode := range [][]string{{"diff"}, {"diff", "--stat"}, {"diff", "--name-only"}} {
		code, out, _ := runCLI(t, dir, mode...)
		if code != 0 || out != "" {
			t.Fatalf("%v: exit=%d out=%q (want 0, empty)", mode, code, out)
		}
	}
}

func TestDiffPathsRequireDashDash(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "diff", "main", "README.md")
	if code != 2 {
		t.Fatalf("exit=%d, want 2 (two positionals without --)", code)
	}
}

func TestDiffStatBothFlagsRejected(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "diff", "--stat", "--name-only")
	if code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
}

func TestDiffPathsOnlyNoRev(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nmore\n"), 0o644)
	code, out, errb := runCLI(t, dir, "diff", "--stat", "--", "README.md")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	want := "README.md +1 -0\n1 files +1 -0\n"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestDiffTwoPathsNoRev(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nmore\n"), 0o644)
	code, _, errb := runCLI(t, dir, "diff", "--", "README.md", "CHANGELOG.md")
	if code != 0 {
		t.Fatalf("two paths after -- must be accepted, exit=%d stderr=%s", code, errb)
	}
}
