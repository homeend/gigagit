package engine

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestDeleteTag(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	if err := repo.CreateTag(context.Background(), "v1.0.0", "", "", false); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Event, 16)
	res, err := DeleteTag{Name: "v1.0.0"}.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "v1.0.0") {
		t.Fatalf("result = %+v", res)
	}
	if exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/tags/v1.0.0").Run() == nil {
		t.Fatal("tag ref still present after delete")
	}
}

func TestDeleteTagRequiresName(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ch := make(chan Event, 4)
	if _, err := (DeleteTag{}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err == nil {
		t.Fatal("empty name must error")
	}
}
