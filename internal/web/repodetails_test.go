package web

import (
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
)

type repoDetailsResp struct {
	Repos []struct {
		Path    string `json:"path"`
		Branch  string `json:"branch"`
		Slow    bool   `json:"slow"`
		Pending bool   `json:"pending"`
	} `json:"repos"`
}

// The list itself carries the MRU timestamp so the picker can show an age
// column without a second round trip.
func TestReposCarriesLastOpened(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	other := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	when := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := repos.Touch(srv.reposPath, other, "", when); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, srv)

	var out struct {
		Repos []struct {
			Path       string    `json:"path"`
			LastOpened time.Time `json:"last_opened"`
		} `json:"repos"`
	}
	if code := getJSON(t, ts, "/api/repos", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(out.Repos) != 1 || !out.Repos[0].LastOpened.Equal(when) {
		t.Fatalf("repos = %+v, want last_opened %s", out.Repos, when)
	}
}

// The details probe walks every registry entry: a plain checkout reports its
// branch, a linked worktree the branch it has out. (A vanished path never
// reaches the probe — the registry drops it on load; HeadAt's own tests
// cover the unreadable cases.)
func TestRepoDetailsReportsBranches(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "side")
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	now := time.Now()
	for _, p := range []string{dir, wt} {
		if err := repos.Touch(srv.reposPath, p, "", now); err != nil {
			t.Fatal(err)
		}
	}
	ts := serve(t, srv)

	var out repoDetailsResp
	if code := getJSON(t, ts, "/api/repos/details", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	got := map[string]string{}
	for _, r := range out.Repos {
		if r.Pending {
			t.Errorf("%s still pending on a local disk", r.Path)
		}
		if r.Slow {
			t.Errorf("%s reported slow in a temp dir", r.Path)
		}
		got[r.Path] = r.Branch
	}
	want := map[string]string{dir: "main", wt: "side"}
	if len(got) != len(want) {
		t.Fatalf("details = %+v, want %+v", got, want)
	}
	for p, b := range want {
		if got[p] != b {
			t.Errorf("branch[%s] = %q, want %q", p, got[p], b)
		}
	}
}

// A probe that outlives the deadline (a hung network mount) is reported
// pending rather than holding the response; a re-poll joins the SAME probe —
// a second request must not start a second read of a hung path — and sees
// the verdict once it lands.
func TestRepoDetailsPendingThenJoins(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(srv.reposPath, dir, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var starts atomic.Int32
	srv.probeRepo = func(path string) (string, bool) {
		starts.Add(1)
		<-release
		return "slow-branch", true
	}
	srv.probeDeadline = 50 * time.Millisecond
	ts := serve(t, srv)

	var out repoDetailsResp
	if code := getJSON(t, ts, "/api/repos/details", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(out.Repos) != 1 || !out.Repos[0].Pending || out.Repos[0].Branch != "" {
		t.Fatalf("first poll = %+v, want pending", out.Repos)
	}
	getJSON(t, ts, "/api/repos/details", &out)
	if !out.Repos[0].Pending {
		t.Fatalf("second poll = %+v, want still pending", out.Repos)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		getJSON(t, ts, "/api/repos/details", &out)
		if !out.Repos[0].Pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("verdict never landed: %+v", out.Repos)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if out.Repos[0].Branch != "slow-branch" || !out.Repos[0].Slow {
		t.Fatalf("landed = %+v, want slow-branch/slow", out.Repos)
	}
	if n := starts.Load(); n != 1 {
		t.Fatalf("probe started %d times, want 1 (re-polls must join)", n)
	}
}
