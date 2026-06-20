package git

import (
	"context"
	"testing"
)

func TestRepoTags(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	gitIn(t, dir, "tag", "v1.0.0")                      // lightweight
	gitIn(t, dir, "tag", "-a", "v2.0.0", "-m", "rel 2") // annotated
	commit := gitOutIn(t, dir, "rev-parse", "--short", "HEAD")

	tags, err := repo.Tags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]bool{}
	for _, tg := range tags {
		byName[tg.Name] = true
		if tg.Target != commit {
			t.Fatalf("%s target = %q, want %q", tg.Name, tg.Target, commit)
		}
		switch tg.Name {
		case "v1.0.0":
			if tg.Annotated {
				t.Fatalf("v1.0.0 must be lightweight")
			}
		case "v2.0.0":
			if !tg.Annotated || tg.Subject != "rel 2" {
				t.Fatalf("v2.0.0 wrong: %+v", tg)
			}
		}
	}
	if !byName["v1.0.0"] || !byName["v2.0.0"] {
		t.Fatalf("missing tags: %+v", tags)
	}
}
