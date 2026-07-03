package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShowDefaultStat(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644)
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "add a")

	code, out, errb := runCLI(t, dir, "show", "HEAD")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// header, one stat line, trailer
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.HasSuffix(lines[0], " add a") {
		t.Fatalf("header = %q", lines[0])
	}
	if lines[1] != "a.txt +2 -0" || lines[2] != "1 files +2 -0" {
		t.Fatalf("stat block wrong:\n%s", out)
	}
}

func TestShowPatchFlag(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "add a")

	code, out, _ := runCLI(t, dir, "show", "--patch", "HEAD")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "+one") || !strings.HasSuffix(strings.Split(out, "\n")[0], " add a") {
		t.Fatalf("patch output wrong:\n%s", out)
	}
}

func TestShowRequiresCommit(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "show")
	if code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
}

func TestShowFileScope(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("two\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "add both")

	code, out, _ := runCLI(t, dir, "show", "HEAD", "--", "a.txt")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if strings.Contains(out, "b.txt") || !strings.Contains(out, "a.txt +1 -0") {
		t.Fatalf("scope wrong:\n%s", out)
	}
}
