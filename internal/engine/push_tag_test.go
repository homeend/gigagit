package engine

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
)

func pushTagFakeRepo(remotes string) (*git.Repo, *gitexec.FakeRunner) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git remote", gitexec.Result{Stdout: remotes})
	f.SetResponse("git push (tag)", gitexec.Result{}) // succeed
	return &git.Repo{Runner: f}, f
}

// pushedToRemote returns the remote of the `git push (tag)` call, if any.
func pushedToRemote(f *gitexec.FakeRunner) (string, bool) {
	for _, c := range f.Calls {
		if c.Name == "git push (tag)" && len(c.Argv) >= 2 {
			return c.Argv[1], true // ["push", "<remote>", "refs/tags/<name>"]
		}
	}
	return "", false
}

func TestPushTagExplicitRemote(t *testing.T) {
	repo, f := pushTagFakeRepo("origin\nbackup\n")
	res, err := PushTag{Name: "v1.0.0", Remote: "backup"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if r, ok := pushedToRemote(f); !ok || r != "backup" {
		t.Fatalf("pushed to %q ok=%v, want backup", r, ok)
	}
}

func TestPushTagSingleRemoteAuto(t *testing.T) {
	repo, f := pushTagFakeRepo("origin\n")
	if _, err := (PushTag{Name: "v1.0.0"}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	if r, _ := pushedToRemote(f); r != "origin" {
		t.Fatalf("auto remote = %q, want origin", r)
	}
}

func TestPushTagMultiRemoteDecider(t *testing.T) {
	repo, f := pushTagFakeRepo("origin\nbackup\n")
	_, err := PushTag{Name: "v1.0.0"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"push-tag-remote": "backup"}})
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := pushedToRemote(f); r != "backup" {
		t.Fatalf("decider remote = %q, want backup", r)
	}
}

func TestPushTagAbort(t *testing.T) {
	repo, f := pushTagFakeRepo("origin\nbackup\n")
	res, err := PushTag{Name: "v1.0.0"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"push-tag-remote": "abort"}})
	if err != nil {
		t.Fatalf("abort should not error: %v", err)
	}
	if res.Changed {
		t.Fatal("abort must be Changed:false")
	}
	if _, ok := pushedToRemote(f); ok {
		t.Fatal("abort must not push")
	}
}

func TestPushTagNoRemotes(t *testing.T) {
	repo, _ := pushTagFakeRepo("")
	if _, err := (PushTag{Name: "v1.0.0"}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("no remotes must error")
	}
}

func TestPushTagRequiresName(t *testing.T) {
	repo, _ := pushTagFakeRepo("origin\n")
	if _, err := (PushTag{}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("empty name must error")
	}
}
