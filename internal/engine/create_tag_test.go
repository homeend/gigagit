package engine

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// engineCatType returns `git cat-file -t <ref>` (tag object type) in dir.
func engineCatType(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "cat-file", "-t", ref).Output()
	if err != nil {
		t.Fatalf("cat-file -t %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func TestCreateTagLightweightAndAnnotated(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)

	ch := make(chan Event, 16)
	if _, err := (CreateTag{Name: "v1.0.0"}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err != nil {
		t.Fatalf("lightweight: %v", err)
	}
	close(ch)
	drain(ch)

	ch2 := make(chan Event, 16)
	res, err := CreateTag{Name: "v2.0.0", Message: "rel2"}.Run(context.Background(), OpDeps{Repo: repo, Events: ch2})
	close(ch2)
	if err != nil {
		t.Fatalf("annotated: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "v2.0.0") {
		t.Fatalf("result = %+v", res)
	}
	// Lightweight points straight at a commit; annotated has its own tag object.
	if typ := engineCatType(t, dir, "v1.0.0"); typ != "commit" {
		t.Fatalf("v1.0.0 type = %q, want commit", typ)
	}
	if typ := engineCatType(t, dir, "v2.0.0"); typ != "tag" {
		t.Fatalf("v2.0.0 type = %q, want tag", typ)
	}
}

func TestCreateTagRequiresName(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ch := make(chan Event, 4)
	if _, err := (CreateTag{Name: ""}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err == nil {
		t.Fatal("empty name must error")
	}
}

func TestCreateTagForceReplacesExisting(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	head := gitOut(t, dir, "rev-parse", "HEAD")
	// Create initial tag
	ch := make(chan Event, 16)
	if _, err := (CreateTag{Name: "v1", Commit: head}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err != nil {
		t.Fatalf("create: %v", err)
	}
	close(ch)
	drain(ch)
	// Without Force, re-tagging the same name must fail (git refuses).
	ch2 := make(chan Event, 16)
	if _, err := (CreateTag{Name: "v1", Commit: head, Message: "x"}).Run(context.Background(), OpDeps{Repo: repo, Events: ch2}); err == nil {
		t.Fatal("re-tag without Force must error")
	}
	close(ch2)
	drain(ch2)
	// With Force, it succeeds and becomes annotated.
	ch3 := make(chan Event, 16)
	res, err := CreateTag{Name: "v1", Commit: head, Message: "annotated now", Force: true}.Run(context.Background(), OpDeps{Repo: repo, Events: ch3})
	close(ch3)
	drain(ch3)
	if err != nil || !res.Changed {
		t.Fatalf("force annotate: res=%+v err=%v", res, err)
	}
	if typ := engineCatType(t, dir, "v1"); typ != "tag" {
		t.Fatalf("tag type = %q, want tag", typ)
	}
}
