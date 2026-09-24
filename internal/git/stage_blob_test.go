package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestStageBlobSetsIndexNotWorktree(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // README.md = "hello\n" committed & in index
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// Modify the working tree so index != working tree.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("WORKING\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stage a DIFFERENT content than either side (proves we set the index to
	// exactly the bytes given, and the working tree is untouched).
	if err := repo.StageBlobs(ctx, []Blob{{Path: "README.md", Content: []byte("STAGED\n")}}); err != nil {
		t.Fatal(err)
	}

	// Working tree unchanged on disk.
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "WORKING\n" {
		t.Fatalf("working tree = %q, want WORKING (untouched)", b)
	}
	// Index now holds STAGED.
	out, err := exec.Command("git", "-C", dir, "show", ":README.md").CombinedOutput()
	if err != nil {
		t.Fatalf("git show :README.md: %v\n%s", err, out)
	}
	if string(out) != "STAGED\n" {
		t.Fatalf("index = %q, want STAGED", out)
	}
}

// countRuns counts the git invocations a verb makes — on a slow filesystem
// (WSL's /mnt drives) every process costs 50–100 ms, so the count IS the
// latency.
type countRuns struct {
	gitexec.Runner
	n int
}

func (c *countRuns) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	c.n++
	return c.Runner.Run(ctx, name, argv)
}

// StageBlobs stages several files in one pass: one ls-files for every mode,
// one hash-object per file (--path is per file), ONE update-index.
func TestStageBlobsBatchesTheIndexWrite(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("base\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "two"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	c := &countRuns{Runner: runner}
	repo := &Repo{Runner: c}
	err := repo.StageBlobs(context.Background(), []Blob{
		{Path: "a.txt", Content: []byte("A\n")},
		{Path: "b.txt", Content: []byte("B\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	for f, want := range map[string]string{"a.txt": "A\n", "b.txt": "B\n", "README.md": "hello\n"} {
		out, err := exec.Command("git", "-C", dir, "show", ":"+f).CombinedOutput()
		if err != nil || string(out) != want {
			t.Fatalf("index %s = %q (%v), want %q", f, out, err, want)
		}
	}
	if c.n != 4 { // ls-files + 2×hash-object + update-index
		t.Fatalf("StageBlobs ran git %d times, want 4", c.n)
	}
	if err := repo.StageBlobs(context.Background(), []Blob{{Path: "nope.txt", Content: []byte("x")}}); err == nil {
		t.Fatal("staging an untracked path must fail")
	}
}

// A Repo that knows its worktree root reads working-tree files without
// asking git where the root is — one process per read on a slow filesystem.
func TestReadWorktreeFileWithAKnownRootRunsNoGit(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	c := &countRuns{Runner: runner}
	repo := &Repo{Runner: c, Root: dir}
	b, err := repo.ReadWorktreeFile(context.Background(), "README.md")
	if err != nil || string(b) != "hello\n" {
		t.Fatalf("read = %q, %v", b, err)
	}
	if c.n != 0 {
		t.Fatalf("a read with a known root ran git %d times, want 0", c.n)
	}
	if _, err := repo.ReadWorktreeFile(context.Background(), "../escape"); err == nil {
		t.Fatal("a path escaping the tree must still be refused")
	}
}
