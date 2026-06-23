package engine

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
)

func delRemoteFakeRepo() (*git.Repo, *gitexec.FakeRunner) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git push delete", gitexec.Result{}) // succeed
	return &git.Repo{Runner: f}, f
}

func pushDeleteCalled(f *gitexec.FakeRunner) (remote, branch string, ok bool) {
	for _, c := range f.Calls {
		if c.Name == "git push delete" && len(c.Argv) >= 4 {
			return c.Argv[1], c.Argv[3], true // ["push", remote, "--delete", branch]
		}
	}
	return "", "", false
}

func TestDeleteRemoteBranchConfirmDeletes(t *testing.T) {
	repo, f := delRemoteFakeRepo()
	res, err := DeleteRemoteBranch{Remote: "origin", Branch: "feat/x"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-remote-branch": "delete"}})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if r, b, ok := pushDeleteCalled(f); !ok || r != "origin" || b != "feat/x" {
		t.Fatalf("push delete called with (%q,%q) ok=%v, want origin/feat/x", r, b, ok)
	}
}

func TestDeleteRemoteBranchAbortDoesNotDelete(t *testing.T) {
	repo, f := delRemoteFakeRepo()
	res, err := DeleteRemoteBranch{Remote: "origin", Branch: "feat/x"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-remote-branch": "abort"}})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Changed {
		t.Fatal("abort must not change anything")
	}
	if _, _, ok := pushDeleteCalled(f); ok {
		t.Fatal("abort must not call push --delete")
	}
}

func TestDeleteRemoteBranchRequiresFields(t *testing.T) {
	repo, _ := delRemoteFakeRepo()
	if _, err := (DeleteRemoteBranch{Branch: "x"}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("missing Remote must error")
	}
	if _, err := (DeleteRemoteBranch{Remote: "origin"}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("missing Branch must error")
	}
}
