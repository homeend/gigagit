package engine

import (
	"context"
	"testing"
)

func TestCheckoutTagDetached(t *testing.T) {
	dir, repo := newRepo(t)
	if err := repo.CreateTag(context.Background(), "v1.0.0", "", ""); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Event, 16)
	res, err := CheckoutTag{Name: "v1.0.0"}.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil || !res.Changed {
		t.Fatalf("detached checkout: res=%+v err=%v", res, err)
	}
	if b := gitOut(t, dir, "branch", "--show-current"); b != "" {
		t.Fatalf("expected detached HEAD, on %q", b)
	}
}

func TestCheckoutTagCreatesBranch(t *testing.T) {
	dir, repo := newRepo(t)
	if err := repo.CreateTag(context.Background(), "v1.0.0", "", ""); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Event, 16)
	if _, err := (CheckoutTag{Name: "v1.0.0", Branch: "rel"}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err != nil {
		t.Fatalf("branch checkout: %v", err)
	}
	close(ch)
	if b := gitOut(t, dir, "branch", "--show-current"); b != "rel" {
		t.Fatalf("on branch %q, want rel", b)
	}
}

func TestCheckoutTagRequiresName(t *testing.T) {
	_, repo := newRepo(t)
	ch := make(chan Event, 4)
	if _, err := (CheckoutTag{}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err == nil {
		t.Fatal("empty name must error")
	}
}
