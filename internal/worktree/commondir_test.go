package worktree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeTree is an in-memory file tree for commonDirAt: dirs and file contents
// keyed by path.
type fakeTree struct {
	dirs  map[string]bool
	files map[string]string
}

func (f fakeTree) isDir(p string) (bool, bool) {
	if f.dirs[p] {
		return true, true
	}
	if _, ok := f.files[p]; ok {
		return false, true
	}
	return false, false
}

func (f fakeTree) read(p string) ([]byte, error) {
	if s, ok := f.files[p]; ok {
		return []byte(s), nil
	}
	return nil, errors.New("missing")
}

func TestCommonDirAtShapes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake tree is spelled in slash paths")
	}
	tree := fakeTree{
		dirs: map[string]bool{
			"/r/main/.git":                          true,
			"/r/main/.git/worktrees/wt":             true,
			"/r/main/.git/modules/sub":              true,
			"/mnt/t/others/test-1/.git":             true,
			"/mnt/t/others/test-1/.git/worktrees/a": true,
		},
		files: map[string]string{
			// a linked worktree with an absolute pointer
			"/r/wt/.git":                          "gitdir: /r/main/.git/worktrees/wt\n",
			"/r/main/.git/worktrees/wt/commondir": "../..\n",
			// a relative pointer (git worktree add --relative-paths)
			"/r/rel/.git": "gitdir: ../main/.git/worktrees/wt\n",
			// a submodule: its gitdir has no commondir, so it is its own repo
			"/r/main/sub/.git": "gitdir: ../.git/modules/sub\n",
			// a worktree created from Windows, seen from WSL
			"/mnt/t/others/test-1.worktrees/a/.git":           "gitdir: T:/others/test-1/.git/worktrees/a\n",
			"/mnt/t/others/test-1/.git/worktrees/a/commondir": "../..\n",
			// a pointer to nowhere, and garbage
			"/r/dangling/.git": "gitdir: /gone/.git/worktrees/x\n",
			"/r/garbage/.git":  "not a pointer\n",
		},
	}
	for path, want := range map[string]string{
		"/r/main":                          "/r/main/.git",
		"/r/wt":                            "/r/main/.git",
		"/r/rel":                           "/r/main/.git",
		"/r/main/sub":                      "/r/main/.git/modules/sub",
		"/mnt/t/others/test-1.worktrees/a": "/mnt/t/others/test-1/.git",
		"/r/dangling":                      "",
		"/r/garbage":                       "",
		"/r/absent":                        "",
	} {
		if got := commonDirAt(tree.isDir, tree.read, "linux", path); got != want {
			t.Errorf("commonDirAt(%s) = %q, want %q", path, got, want)
		}
	}
}

// The real filesystem path, against a real git worktree: the checkout and its
// linked worktree report one common dir.
func TestCommonDirAtRealWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	main := filepath.Join(root, "main")
	wt := filepath.Join(root, "wt")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	run(main, "init", "-q", "-b", "main")
	run(main, "commit", "-q", "--allow-empty", "-m", "c")
	run(main, "worktree", "add", "-q", "-b", "side", wt)

	a, b := CommonDirAt(runtime.GOOS, main), CommonDirAt(runtime.GOOS, wt)
	if a == "" || a != b {
		t.Fatalf("checkout and worktree must share one common dir: %q vs %q", a, b)
	}
	if CommonDirAt(runtime.GOOS, root) != "" {
		t.Error("a plain directory has no common dir")
	}
}
