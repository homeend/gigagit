package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

func gitAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func revAt(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return string(out)
}

func cloneOnMainBehindOrigin(t *testing.T) (string, *git.Repo) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")

	gitAt(t, root, "init", "--bare", origin)
	gitAt(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitAt(t, seed, "checkout", "-b", "main")
	gitAt(t, seed, "add", ".")
	gitAt(t, seed, "commit", "-m", "v1")
	gitAt(t, seed, "push", "-u", "origin", "main")

	gitAt(t, root, "clone", origin, clone)
	gitAt(t, clone, "checkout", "main")

	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitAt(t, seed, "add", ".")
	gitAt(t, seed, "commit", "-m", "v2")
	gitAt(t, seed, "push", "origin", "main")

	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", clone, observ.NewRing(50))}
	return clone, repo
}

func TestSmartPullCurrentBranchFastForward(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart pull: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if revAt(t, clone, "main") != revAt(t, clone, "origin/main") {
		t.Fatal("main was not fast-forwarded to origin/main")
	}
}

func TestSmartPullCurrentBranchNonFastForwardRebase(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("local\n"), 0o644)
	gitAt(t, clone, "add", ".")
	gitAt(t, clone, "commit", "-m", "local")

	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"non-fast-forward": "rebase"}})
	if err != nil {
		t.Fatalf("smart pull (rebase): %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(clone, "local.txt")); err != nil {
		t.Fatalf("local.txt missing after rebase: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "v2\n" {
		t.Fatalf("f.txt = %q, want v2 (remote change applied)", b)
	}
}

func TestSmartPullBackgroundFastForwardsOtherBranch(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	root := filepath.Dir(clone)
	seed := filepath.Join(root, "seed")

	gitAt(t, seed, "checkout", "-b", "dev")
	gitAt(t, clone, "fetch", "origin")
	gitAt(t, clone, "branch", "dev", "origin/main")
	gitAt(t, seed, "commit", "--allow-empty", "-m", "dev-advance")
	gitAt(t, seed, "push", "-u", "origin", "dev")

	res, err := SmartPull{Branch: "dev", Intent: PullInBackground}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart pull background: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "main" {
		t.Fatalf("current branch = %q, want main (background must not checkout)", cur)
	}
}

func TestSmartPullStayStashesAndMovesToTarget(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	gitAt(t, clone, "branch", "feature", "origin/main")
	gitAt(t, clone, "push", "-u", "origin", "feature")
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("dirty-local\n"), 0o644)

	res, err := SmartPull{Branch: "feature", Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart pull stay: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "feature" {
		t.Fatalf("current branch = %q, want feature (PullAndStay ends on target)", cur)
	}
	dirty, _ := repo.IsDirty(context.Background())
	if !dirty {
		t.Fatal("expected dirty change restored on feature")
	}
}
