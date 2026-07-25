package web

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
)

func TestTouchMRURecordsServedRepo(t *testing.T) {
	dir := newRepoDir(t, 1)
	sp := filepath.Join(t.TempDir(), "repos.toml")
	touchMRU(context.Background(), domain.Open(dir), sp)
	es := repos.Load(sp)
	if len(es) != 1 || es[0].Path != dir {
		t.Fatalf("registry = %+v, want [%s]", es, dir)
	}
}

func TestRerootRecordsNewRoot(t *testing.T) {
	dir := newRepoDir(t, 2)
	wt := addWorktree(t, dir, "side")
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	ts := serve(t, srv)

	if code := postJSON(t, ts, "/api/reroot", rerootBody(wt), "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("reroot code = %d", code)
	}
	found := false
	for _, e := range repos.Load(srv.reposPath) {
		if e.Path == wt {
			found = true
		}
	}
	if !found {
		t.Fatalf("new root not recorded: %+v", repos.Load(srv.reposPath))
	}
}

func TestReposEndpoint(t *testing.T) {
	dir := newRepoDir(t, 1)
	other := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(srv.reposPath, other, time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, srv)

	var out struct {
		Repos []struct{ Path, Name string } `json:"repos"`
	}
	if code := getJSON(t, ts, "/api/repos", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(out.Repos) != 1 || out.Repos[0].Path != other || out.Repos[0].Name == "" {
		t.Fatalf("repos = %+v", out.Repos)
	}
}
