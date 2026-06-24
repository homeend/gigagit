package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// newConflictRepo builds a real repo paused on a merge with a UU (both modified)
// and a DU (deleted by us, modified by them) conflict.
func newConflictRepo(t *testing.T) (string, *git.Repo) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil && args[0] != "merge" {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) { os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644) }
	run("init", "-q", "-b", "main")
	write("uu.txt", "base\n")
	write("md.txt", "base\n")
	run("add", "-A")
	run("commit", "-qm", "base")
	run("checkout", "-q", "-b", "feature")
	write("uu.txt", "theirs\n")
	write("md.txt", "theirs-mod\n")
	run("add", "-A")
	run("commit", "-qm", "feature")
	run("checkout", "-q", "main")
	write("uu.txt", "ours\n")
	run("add", "-A")
	run("rm", "-q", "md.txt")
	run("commit", "-qm", "main")
	run("merge", "feature") // conflicts (exit 1) — tolerated above
	return dir, &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
}

func TestResolveConflictKeepTheirs(t *testing.T) {
	dir, repo := newConflictRepo(t)
	ctx := context.Background()
	_, err := ResolveConflict{Path: "uu.txt", Action: KeepTheirs}.Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uu.txt")); string(b) != "theirs\n" {
		t.Errorf("uu.txt = %q, want theirs", b)
	}
	st, _ := repo.Status(ctx)
	if len(st.Conflicts()) != 1 { // only md.txt remains
		t.Errorf("want 1 remaining conflict, got %d", len(st.Conflicts()))
	}
}

func TestResolveConflictDelete(t *testing.T) {
	dir, repo := newConflictRepo(t)
	ctx := context.Background()
	if _, err := (ResolveConflict{Path: "md.txt", Action: DeleteFile}).Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "md.txt")); !os.IsNotExist(err) {
		t.Error("md.txt should be deleted")
	}
}

func TestContinueOpFinishesMerge(t *testing.T) {
	_, repo := newConflictRepo(t)
	ctx := context.Background()
	ev := func() OpDeps { return OpDeps{Repo: repo, Events: make(chan Event, 16)} }
	// resolve both conflicts, then continue the merge (exercises the
	// RunEnv/GIT_EDITOR=true editor-safe --continue path).
	if _, err := (ResolveConflict{Path: "uu.txt", Action: KeepTheirs}).Run(ctx, ev()); err != nil {
		t.Fatal(err)
	}
	if _, err := (ResolveConflict{Path: "md.txt", Action: DeleteFile}).Run(ctx, ev()); err != nil {
		t.Fatal(err)
	}
	if _, err := (ContinueOp{}).Run(ctx, ev()); err != nil {
		t.Fatal(err)
	}
	if ok, _ := repo.MergeInProgress(ctx, ""); ok {
		t.Error("merge should be finished after continue")
	}
	st, _ := repo.Status(ctx)
	if len(st.Conflicts()) != 0 {
		t.Errorf("continue should leave a clean tree, got %d conflicts", len(st.Conflicts()))
	}
	// HEAD is now a merge commit: two parents.
	out, err := repo.Runner.Run(ctx, "git rev-list parents",
		[]string{"rev-list", "--parents", "-n", "1", "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Fields(out.Stdout)); n != 3 { // commit + 2 parents
		t.Errorf("HEAD should be a merge commit (self + 2 parents), got %d fields: %q", n, out.Stdout)
	}
}

func TestAbortOpClearsConflict(t *testing.T) {
	_, repo := newConflictRepo(t)
	ctx := context.Background()
	if _, err := (AbortOp{}).Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)}); err != nil {
		t.Fatal(err)
	}
	st, _ := repo.Status(ctx)
	if len(st.Conflicts()) != 0 {
		t.Errorf("abort should clear conflicts, got %d", len(st.Conflicts()))
	}
}
