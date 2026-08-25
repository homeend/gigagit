package web

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type worktreesResp struct {
	Worktrees []struct {
		Path     string `json:"path"`
		Branch   string `json:"branch"`
		Head     string `json:"head"`
		Detached bool   `json:"detached"`
		Bare     bool   `json:"bare"`
	} `json:"worktrees"`
}

type tagsResp struct {
	Tags []struct {
		Name      string `json:"name"`
		Target    string `json:"target"`
		Annotated bool   `json:"annotated"`
		Subject   string `json:"subject"`
	} `json:"tags"`
	Truncated bool `json:"truncated"`
}

func TestWorktreesEndpoint(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	second := filepath.Join(t.TempDir(), "wt2")
	gitRun(t, dir, "worktree", "add", "-b", "w2", second)
	ts := serve(t, New(domain.Open(dir)))

	var body worktreesResp
	if code := getJSON(t, ts, "/api/worktrees", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Worktrees) != 2 {
		t.Fatalf("worktrees = %+v", body.Worktrees)
	}
	byBranch := map[string]bool{}
	for _, w := range body.Worktrees {
		if w.Path == "" || w.Head == "" {
			t.Errorf("missing path/head: %+v", w)
		}
		if w.Bare || w.Detached {
			t.Errorf("unexpected bare/detached: %+v", w)
		}
		byBranch[w.Branch] = true
	}
	if !byBranch["main"] || !byBranch["w2"] {
		t.Errorf("branches seen = %v, want main and w2", byBranch)
	}
}

func TestTagsEndpoint(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "tag", "light1")
	gitRun(t, dir, "tag", "light2")
	gitRun(t, dir, "tag", "-a", "annot1", "-m", "release notes here")
	ts := serve(t, New(domain.Open(dir)))

	var body tagsResp
	if code := getJSON(t, ts, "/api/tags", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if body.Truncated {
		t.Error("truncated=true for 3 tags")
	}
	if len(body.Tags) != 3 {
		t.Fatalf("tags = %+v", body.Tags)
	}
	seenAnnot := false
	for _, tg := range body.Tags {
		if tg.Name == "" || tg.Target == "" {
			t.Errorf("missing name/target: %+v", tg)
		}
		if tg.Name == "annot1" {
			seenAnnot = true
			if !tg.Annotated || tg.Subject != "release notes here" {
				t.Errorf("annot1 = %+v", tg)
			}
		}
	}
	if !seenAnnot {
		t.Error("annot1 missing")
	}
}

func TestTagsEndpointCap(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	for i := 0; i < 105; i++ {
		gitRun(t, dir, "tag", fmt.Sprintf("t%03d", i))
	}
	ts := serve(t, New(domain.Open(dir)))
	var body tagsResp
	if code := getJSON(t, ts, "/api/tags", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Tags) != 100 {
		t.Errorf("len = %d, want 100 (maxTagRows)", len(body.Tags))
	}
	if !body.Truncated {
		t.Error("truncated = false, want true")
	}
}
