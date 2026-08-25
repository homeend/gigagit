package engine

import (
	"context"
	"testing"
)

func TestCheckoutDetached(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	if err := repo.CreateTag(context.Background(), "v1.0.0", "", "", false); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Event, 16)
	res, err := Checkout{Ref: "v1.0.0"}.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil || !res.Changed {
		t.Fatalf("detached checkout: res=%+v err=%v", res, err)
	}
	if b := gitOut(t, dir, "branch", "--show-current"); b != "" {
		t.Fatalf("expected detached HEAD, on %q", b)
	}
}

func TestCheckoutCreatesBranch(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	if err := repo.CreateTag(context.Background(), "v1.0.0", "", "", false); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Event, 16)
	if _, err := (Checkout{Ref: "v1.0.0", Branch: "rel"}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err != nil {
		t.Fatalf("branch checkout: %v", err)
	}
	close(ch)
	if b := gitOut(t, dir, "branch", "--show-current"); b != "rel" {
		t.Fatalf("on branch %q, want rel", b)
	}
}

// A plain commit SHA (not a tag) proves Checkout is commit-ish-agnostic — the
// reflog recovery actions pass a reflog entry's SHA.
func TestCheckoutBySHACreatesBranch(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	sha := gitOut(t, dir, "rev-parse", "HEAD")
	ch := make(chan Event, 16)
	if _, err := (Checkout{Ref: sha, Branch: "recovered"}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err != nil {
		t.Fatalf("checkout by SHA: %v", err)
	}
	close(ch)
	if b := gitOut(t, dir, "branch", "--show-current"); b != "recovered" {
		t.Fatalf("on branch %q, want recovered", b)
	}
}

func TestCheckoutRequiresRef(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ch := make(chan Event, 4)
	if _, err := (Checkout{}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err == nil {
		t.Fatal("empty ref must error")
	}
}
