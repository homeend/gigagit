package engine

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

func delTagFakeRepo(remotes string) (*git.Repo, *gitexec.FakeRunner) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git remote", gitexec.Result{Stdout: remotes})
	f.SetResponse("git push delete tag", gitexec.Result{})
	return &git.Repo{Runner: f}, f
}

func deleteTagCalled(f *gitexec.FakeRunner) (remote, tag string, ok bool) {
	for _, c := range f.Calls {
		if c.Name == "git push delete tag" && len(c.Argv) >= 4 {
			return c.Argv[1], c.Argv[3], true // ["push", remote, "--delete", "refs/tags/<tag>"]
		}
	}
	return "", "", false
}

func TestDeleteRemoteTagSingleRemoteConfirm(t *testing.T) {
	repo, f := delTagFakeRepo("origin\n")
	res, err := DeleteRemoteTag{Tag: "v1.0.0"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-remote-tag": "delete"}})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	r, ref, ok := deleteTagCalled(f)
	if !ok || r != "origin" || ref != "refs/tags/v1.0.0" {
		t.Fatalf("push delete tag called with (%q,%q) ok=%v", r, ref, ok)
	}
}

func TestDeleteRemoteTagAbortDoesNotPush(t *testing.T) {
	repo, f := delTagFakeRepo("origin\n")
	res, err := DeleteRemoteTag{Tag: "v1.0.0"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-remote-tag": "abort"}})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Changed {
		t.Fatal("abort must not change anything")
	}
	if _, _, ok := deleteTagCalled(f); ok {
		t.Fatal("abort must not push a deletion")
	}
}

func TestDeleteRemoteTagMultiRemotePick(t *testing.T) {
	repo, f := delTagFakeRepo("origin\nbackup\n")
	_, err := DeleteRemoteTag{Tag: "v1.0.0"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-remote-tag-remote": "backup", "delete-remote-tag": "delete"}})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if r, _, ok := deleteTagCalled(f); !ok || r != "backup" {
		t.Fatalf("pushed to %q ok=%v, want backup", r, ok)
	}
}

func TestDeleteRemoteTagRequiresTag(t *testing.T) {
	repo, _ := delTagFakeRepo("origin\n")
	if _, err := (DeleteRemoteTag{}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("missing Tag must error")
	}
}
