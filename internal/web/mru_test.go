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

type reposResp struct {
	Repos []struct {
		Path    string `json:"path"`
		Name    string `json:"name"`
		Current bool   `json:"current"`
	} `json:"repos"`
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

	var out reposResp
	if code := getJSON(t, ts, "/api/repos", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(out.Repos) != 1 || out.Repos[0].Path != other || out.Repos[0].Name == "" {
		t.Fatalf("repos = %+v", out.Repos)
	}
}

// The picker filters on this flag, so the served repo must carry it and its
// neighbours must not — otherwise "switch repo" offers the repo already open
// and switching to it does nothing.
func TestReposMarksServedRepoCurrent(t *testing.T) {
	dir := newRepoDir(t, 1)
	other := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	now := time.Now()
	for _, p := range []string{dir, other} {
		if err := repos.Touch(srv.reposPath, p, now); err != nil {
			t.Fatal(err)
		}
	}
	ts := serve(t, srv)

	var out reposResp
	if code := getJSON(t, ts, "/api/repos", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(out.Repos) != 2 {
		t.Fatalf("repos = %+v, want both", out.Repos)
	}
	seen := map[string]bool{}
	for _, r := range out.Repos {
		seen[r.Path] = r.Current
	}
	if !seen[dir] {
		t.Errorf("served repo %s not marked current: %+v", dir, out.Repos)
	}
	if seen[other] {
		t.Errorf("a different repo %s marked current: %+v", other, out.Repos)
	}
}
