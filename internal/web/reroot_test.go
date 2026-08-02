package web

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
)

func rerootBody(path string) string {
	return fmt.Sprintf(`{"path":%q}`, path)
}

type repoResp struct {
	Name     string `json:"name"`
	Worktree string `json:"worktree"`
	Branch   string `json:"branch"`
}

func TestRerootToWorktree(t *testing.T) {
	dir := newRepoDir(t, 2)
	wt := addWorktree(t, dir, "side")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	if hb := headBranchOf(t, ts); hb != "main" { // warms the feed cache
		t.Fatalf("head before reroot = %q", hb)
	}
	var out repoResp
	if code := postJSON(t, ts, "/api/reroot", rerootBody(wt), "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("reroot code = %d", code)
	}
	if out.Worktree != wt || out.Branch != "side" {
		t.Fatalf("reroot resp = %+v", out)
	}
	var repo repoResp
	getJSON(t, ts, "/api/repo", &repo)
	if repo.Worktree != wt {
		t.Errorf("/api/repo worktree = %q, want %q", repo.Worktree, wt)
	}
	// feed rebuilt against the new root: HEAD decoration moved to side
	if hb := headBranchOf(t, ts); hb != "side" {
		t.Errorf("head after reroot = %q, want side (feed not reset?)", hb)
	}
}

func TestRerootToMRURepo(t *testing.T) {
	dir := newRepoDir(t, 1)
	other := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(srv.reposPath, other, time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, srv)

	var out repoResp
	if code := postJSON(t, ts, "/api/reroot", rerootBody(other), "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("reroot code = %d", code)
	}
	if out.Worktree != other {
		t.Errorf("worktree = %q, want %q", out.Worktree, other)
	}
}

// A path that is neither allowlisted nor a repository flows into the custom
// lane and fails PREFLIGHT (409) — the old root must keep serving. (Was a
// 404 "unknown target" before the custom-path lane existed.)
func TestRerootUnknownTarget(t *testing.T) {
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	if code := postJSON(t, ts, "/api/reroot", rerootBody(filepath.Join(t.TempDir(), "nope")), "application/json", "", nil); code != http.StatusConflict {
		t.Fatalf("code = %d, want 409", code)
	}
	var repo repoResp
	getJSON(t, ts, "/api/repo", &repo)
	if repo.Worktree != dir {
		t.Errorf("old root gone: %q", repo.Worktree)
	}
}

// The palette's "open repo (path)" lane: a raw filesystem path outside the
// worktree/MRU allowlist re-roots when it IS a repository.
func TestRerootCustomPath(t *testing.T) {
	dir := newRepoDir(t, 1)
	other := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml") // empty MRU — nothing allowlists `other`
	ts := serve(t, srv)

	var out repoResp
	if code := postJSON(t, ts, "/api/reroot", rerootBody(other), "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("custom-path reroot code = %d", code)
	}
	if out.Worktree != other {
		t.Errorf("worktree = %q, want %q", out.Worktree, other)
	}
}

// A leading dash never reaches git argv through the custom lane.
func TestRerootDashPathRefused(t *testing.T) {
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	for _, p := range []string{"-C/evil", "  ", ""} {
		if code := postJSON(t, ts, "/api/reroot", rerootBody(p), "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("path %q: code = %d, want 400", p, code)
		}
	}
}

func TestExpandHome(t *testing.T) {
	cases := []struct{ path, home, want string }{
		{"~", "/home/u", "/home/u"},
		{"~/r", "/home/u", filepath.Join("/home/u", "r")},
		{"~/a/b", "/home/u", filepath.Join("/home/u", "a", "b")},
		{"/abs/p", "/home/u", "/abs/p"},
		{"~user/r", "/home/u", "~user/r"}, // ~user unsupported, passed through
		{"~/r", "", "~/r"},                // no home: untouched
	}
	for _, c := range cases {
		if got := expandHome(c.path, c.home); got != c.want {
			t.Errorf("expandHome(%q, %q) = %q, want %q", c.path, c.home, got, c.want)
		}
	}
}

func TestRerootRefusedWhileOpLive(t *testing.T) {
	dir := newRepoDir(t, 2)
	wt := addWorktree(t, dir, "side")
	gitRun(t, dir, "branch", "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/reroot", rerootBody(wt), "application/json", "", nil); code != http.StatusConflict {
		t.Fatalf("reroot during op = %d, want 409", code)
	}
	postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"abort"}`, "application/json", "", nil)
	readSSE(t, ts, opID, 30*time.Second)
	if code := postJSON(t, ts, "/api/reroot", rerootBody(wt), "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("reroot after op = %d, want 200", code)
	}
}

func TestRerootBrokenTargetKeepsServing(t *testing.T) {
	dir := newRepoDir(t, 1)
	notARepo := t.TempDir() // exists (survives repos.Load pruning) but is no repository
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(srv.reposPath, notARepo, time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, srv)

	if code := postJSON(t, ts, "/api/reroot", rerootBody(notARepo), "application/json", "", nil); code != http.StatusConflict {
		t.Fatalf("broken target code = %d, want 409", code)
	}
	var repo repoResp
	getJSON(t, ts, "/api/repo", &repo)
	if repo.Worktree != dir {
		t.Errorf("old root gone after failed reroot: %q", repo.Worktree)
	}
}

func TestRerootDropsOldOpRecord(t *testing.T) {
	dir := newRepoDir(t, 2)
	wt := addWorktree(t, dir, "side")
	ts := serve(t, New(domain.Open(dir)))

	opID := startOpBody(t, ts, `{"op":"stash","message":"x"}`) // fails fast (nothing to stash) — a finished run
	readSSE(t, ts, opID, 30*time.Second)
	if code := postJSON(t, ts, "/api/reroot", rerootBody(wt), "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("reroot code = %d", code)
	}
	resp, err := http.Get(ts.URL + "/api/op/" + opID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("old op events after reroot = %d, want 404", resp.StatusCode)
	}
}

func TestRerootWriteGuard(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/reroot", rerootBody(dir), "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("non-JSON = %d, want 415", code)
	}
	if code := postJSON(t, ts, "/api/reroot", rerootBody(dir), "application/json", "http://evil.example", nil); code != http.StatusForbidden {
		t.Errorf("cross-origin = %d, want 403", code)
	}
}
