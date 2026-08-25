package domain

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/textdiff"
)

// TestDiffCRLFWorkingCopyMatchesGit reproduces the real-world whole-file-green
// bug: under core.autocrlf=input a working copy can be CRLF while `git show`
// emits LF. git diff normalizes and sees the file unchanged; gg's diff (ShowFile
// HEAD vs the raw disk bytes) must agree — every row Same, no spurious changes.
func TestDiffCRLFWorkingCopyMatchesGit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "core.autocrlf", "input")
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "lf")

	// Rewrite the working copy with CRLF line endings, same text.
	if err := os.WriteFile(path, []byte("alpha\r\nbeta\r\ngamma\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// `git status` flags the file (pending CRLF→LF normalization), so it shows
	// up in the Status panel and the user opens its diff — but `git diff`
	// normalizes and reports NO content change. gg's diff must agree.
	out, err := exec.Command("git", "-C", dir, "diff", "HEAD", "--", "f.txt").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("precondition: git diff should report no content change, got:\n%s", out)
	}

	svc := New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})
	differ := NewDiffer(DifferOptions{Enhanced: true}, nil)
	ctx := context.Background()
	res, err := differ.Diff(ctx, Request{
		Old: func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, "HEAD", "f.txt") },
		New: func(ctx context.Context) ([]byte, error) { return os.ReadFile(path) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range res.Result.Rows {
		if row.Kind != textdiff.Same {
			t.Errorf("row %d kind = %v (Left=%q Right=%q), want Same — gg should agree with git that the file is unchanged",
				i, row.Kind, row.Left, row.Right)
		}
	}
}
