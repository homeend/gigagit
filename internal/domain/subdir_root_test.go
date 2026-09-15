package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
)

// subdirRepo builds a repo with one committed file at the root and an
// untracked src/xxx.txt, returning the repo root.
func subdirRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-m", "base")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "xxx.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// gg run from a subdirectory must still act on the repo, not on the
// subdirectory: every gg surface reports worktree-root-relative paths (git
// status --porcelain is root-relative), so a git command run with the
// subdirectory as its cwd resolves "src/xxx.txt" as "src/src/xxx.txt" and
// fails with a pathspec error.
func TestOpenFromSubdirRootsAtToplevel(t *testing.T) {
	t.Parallel()
	root := subdirRepo(t)
	sub := filepath.Join(root, "src")

	svc := Open(sub)
	ctx := context.Background()

	top, err := svc.TopLevel(ctx)
	if err != nil {
		t.Fatalf("toplevel: %v", err)
	}
	if !samePath(t, top, root) {
		t.Fatalf("TopLevel = %q, want repo root %q", top, root)
	}

	// The path Status reports must be the path Stage accepts.
	st, err := svc.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var path string
	for _, f := range st.Files {
		if filepath.Base(f.Path) == "xxx.txt" {
			path = f.Path
		}
	}
	if path != "src/xxx.txt" {
		t.Fatalf("Status path = %q, want src/xxx.txt (files %+v)", path, st.Files)
	}
	if _, err := svc.Execute(ctx, engine.Stage{Paths: []string{path}}, nil, nil); err != nil {
		t.Fatalf("Stage %q from subdir: %v", path, err)
	}

	st, err = svc.Status(ctx)
	if err != nil {
		t.Fatalf("status after stage: %v", err)
	}
	for _, f := range st.Files {
		if f.Path == "src/xxx.txt" && f.Staged == 'A' {
			return
		}
	}
	t.Fatalf("src/xxx.txt not staged: %+v", st.Files)
}

func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return filepath.Clean(ra) == filepath.Clean(rb)
}

// A directory that is not inside a worktree keeps the workdir it was given,
// so the friendly "not a repository" startup errors still fire. This pins the
// fallback: resolveRoot must never invent a root.
func TestOpenOutsideARepoKeepsWorkdir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if got := Open(dir).Root(); got != dir {
		t.Fatalf("Root() = %q outside a repo, want the given workdir %q", got, dir)
	}
}

// A Service built by New has no workdir, and Root must say so ("") rather
// than guessing — frontends pass a pathspec through untouched when it does.
func TestNewServiceHasNoRoot(t *testing.T) {
	t.Parallel()
	if got := New(nil).Root(); got != "" {
		t.Fatalf("Root() = %q for a New service, want \"\"", got)
	}
}
