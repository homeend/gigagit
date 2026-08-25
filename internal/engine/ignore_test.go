package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEscapeIgnorePattern(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"a[1].log":  `a\[1].log`,
		"a*b":       `a\*b`,
		"a?b":       `a\?b`,
		`a\b`:       `a\\b`,
		"plain.txt": "plain.txt",
	}
	for in, want := range cases {
		if got := escapeIgnorePattern(in); got != want {
			t.Errorf("escapeIgnorePattern(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIgnoreLine(t *testing.T) {
	t.Parallel()
	if got := ignoreLine("a/b.log", false); got != "/a/b.log" {
		t.Errorf("exact = %q", got)
	}
	if got := ignoreLine("a/b.log", true); got != "*.log" {
		t.Errorf("ext = %q", got)
	}
	if got := ignoreLine("a[1].log", false); got != `/a\[1].log` {
		t.Errorf("exact-escaped = %q", got)
	}
}

func TestAlreadyIgnored(t *testing.T) {
	t.Parallel()
	content := []byte("# a comment\n\n/a/b.log\n*.tmp\n")
	if !alreadyIgnored(content, "/a/b.log") {
		t.Error("present exact line not detected")
	}
	if !alreadyIgnored(content, "*.tmp") {
		t.Error("present ext line not detected")
	}
	if alreadyIgnored(content, "/a/b") {
		t.Error("substring must not match")
	}
	if alreadyIgnored(content, "# a comment") {
		t.Error("comment line must not count as a pattern")
	}
	if alreadyIgnored(nil, "/x") {
		t.Error("empty content")
	}
}

func TestAppendIgnoreLine(t *testing.T) {
	t.Parallel()
	if got := appendIgnoreLine(nil, "/x"); string(got) != "/x\n" {
		t.Errorf("empty = %q", got)
	}
	if got := appendIgnoreLine([]byte("/a\n"), "/x"); string(got) != "/a\n/x\n" {
		t.Errorf("trailing-nl = %q", got)
	}
	if got := appendIgnoreLine([]byte("/a"), "/x"); string(got) != "/a\n/x\n" {
		t.Errorf("no-trailing-nl = %q", got)
	}
}

func gitStatus(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	return string(out)
}

func TestIgnoreExactRemovesUntrackedFromStatus(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "out.log"), []byte("x\n"), 0o644)

	res, err := Ignore{Path: "out.log"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Changed {
		t.Fatalf("first run should report Changed")
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), "/out.log") {
		t.Fatalf(".gitignore = %q", gi)
	}
	if strings.Contains(gitStatus(t, dir), "out.log") {
		t.Fatalf("out.log still in status:\n%s", gitStatus(t, dir))
	}

	// Idempotent: second run is a no-op, no duplicate line.
	res2, _ := Ignore{Path: "out.log"}.Run(context.Background(), OpDeps{Repo: repo})
	if res2.Changed {
		t.Fatalf("second run should be no-op")
	}
	gi2, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if strings.Count(string(gi2), "/out.log") != 1 {
		t.Fatalf("duplicate line: %q", gi2)
	}
}

func TestIgnoreNestedPathActuallyIgnored(t *testing.T) {
	t.Parallel()
	// The dominant monorepo case: a nested untracked file. The anchored "/path"
	// is correct only because git runs from the repo root, so f.Path is
	// root-relative — this proves that end-to-end.
	dir, repo := newRepo(t)
	os.MkdirAll(filepath.Join(dir, "sub", "dir"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "dir", "out.log"), []byte("x\n"), 0o644)

	if _, err := (Ignore{Path: "sub/dir/out.log"}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("run: %v", err)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), "/sub/dir/out.log") {
		t.Fatalf(".gitignore = %q", gi)
	}
	if strings.Contains(gitStatus(t, dir), "sub/dir/out.log") {
		t.Fatalf("nested file not ignored:\n%s", gitStatus(t, dir))
	}
}

func TestIgnoreMetacharFilenameActuallyIgnored(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "a[1].log"), []byte("x\n"), 0o644)

	if _, err := (Ignore{Path: "a[1].log"}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(gitStatus(t, dir), "a[1].log") {
		t.Fatalf("metachar file not ignored (unescaped [):\n%s", gitStatus(t, dir))
	}
}

func TestIgnoreExtensionRemovesAllMatching(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "one.tmp"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "two.tmp"), []byte("y\n"), 0o644)

	if _, err := (Ignore{Path: "one.tmp", Ext: true}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("run: %v", err)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), "*.tmp") {
		t.Fatalf(".gitignore = %q", gi)
	}
	st := gitStatus(t, dir)
	if strings.Contains(st, "one.tmp") || strings.Contains(st, "two.tmp") {
		t.Fatalf("extension did not ignore both:\n%s", st)
	}
}
