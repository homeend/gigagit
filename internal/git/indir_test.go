package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

// InDir must prefix every invocation with -C <dir> and leave the rest of
// the argv untouched.
func TestInDirPrefixesArgv(t *testing.T) {
	t.Parallel()
	fake := gitexec.NewFakeRunner()
	fake.SetResponse("git symbolic-ref", gitexec.Result{Stdout: "other\n"})
	repo := &Repo{Runner: fake}
	wt := repo.InDir("/elsewhere/wt")
	if _, err := wt.CurrentBranch(context.Background()); err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.Calls))
	}
	got := fake.Calls[0].Argv
	if len(got) < 3 || got[0] != "-C" || got[1] != "/elsewhere/wt" {
		t.Fatalf("argv = %v, want -C /elsewhere/wt …", got)
	}
	if !slices.Contains(got[2:], "symbolic-ref") {
		t.Fatalf("argv after the prefix = %v, want the CurrentBranch verb", got[2:])
	}
	if wt.Root != "/elsewhere/wt" {
		t.Fatalf("Root = %q, want the dir", wt.Root)
	}
}

// Against a real repo with a linked worktree, the view answers for the
// OTHER worktree: its branch and its dirty state, not ours.
func TestInDirReadsTheOtherWorktree(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	wt := filepath.Join(filepath.Dir(dir), "wt-other")
	c := exec.Command("git", "-C", dir, "worktree", "add", "-b", "other", wt, "main")
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("x\n"), 0o644)

	view := repo.InDir(wt)
	ctx := context.Background()
	if b, err := view.CurrentBranch(ctx); err != nil || b != "other" {
		t.Fatalf("CurrentBranch = %q, %v; want other", b, err)
	}
	st, err := view.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Counts().Untracked != 1 {
		t.Fatalf("other worktree untracked = %d, want 1 (%+v)", st.Counts().Untracked, st.Files)
	}
	if top, err := view.TopLevel(ctx); err != nil || top != filepath.Clean(wt) {
		t.Fatalf("TopLevel = %q, %v; want %s", top, err, wt)
	}
	// The original repo is untouched.
	if b, err := repo.CurrentBranch(ctx); err != nil || b != "main" {
		t.Fatalf("original CurrentBranch = %q, %v; want main", b, err)
	}
}
