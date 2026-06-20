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
	_, repo := newRepo(t)
	ch := make(chan Event, 4)
	if _, err := (CreateTag{Name: ""}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err == nil {
		t.Fatal("empty name must error")
	}
}
