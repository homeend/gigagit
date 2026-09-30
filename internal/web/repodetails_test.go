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
		Path    string         `json:"path"`
		Branch  string         `json:"branch"`
		Slow    bool           `json:"slow"`
		Pending bool           `json:"pending"`
		Project *repos.Project `json:"project"`
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
	srv.probeRepo = func(path string) repoVerdict {
		starts.Add(1)
		<-release
		return repoVerdict{branch: "slow-branch", slow: true}
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

// The switcher groups a checkout with its linked worktrees: the details carry
// each entry's project, computed from the common dirs the probes read — a
// repository with no remote groups too, named after its main checkout. An
// entry the list alone can already place (a remote name) carries its project
// in /api/repos, before any probe.
func TestRepoDetailsCarryProjects(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "side")
	other := newRepoDir(t, 1)
	cloneA, cloneB := newRepoDir(t, 1), newRepoDir(t, 1)
	srv := New(domain.Open(other))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	now := time.Now()
	for i, e := range []struct{ path, remote string }{
		{dir, repos.NoRemote}, {wt, ""}, {other, repos.NoRemote}, {cloneA, "proj"}, {cloneB, "proj"},
	} {
		if err := repos.Touch(srv.reposPath, e.path, e.remote, now.Add(-time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	ts := serve(t, srv)

	var list struct {
		Repos []struct {
			Path    string         `json:"path"`
			Project *repos.Project `json:"project"`
		} `json:"repos"`
	}
	getJSON(t, ts, "/api/repos", &list)
	byPath := map[string]*repos.Project{}
	for _, r := range list.Repos {
		byPath[r.Path] = r.Project
	}
	if a, b := byPath[cloneA], byPath[cloneB]; a == nil || b == nil || a.Key != b.Key || a.Label != "proj" {
		t.Errorf("the list must already group the clones by remote: %+v %+v", a, b)
	}

	var out repoDetailsResp
	if code := getJSON(t, ts, "/api/repos/details", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	proj := map[string]*repos.Project{}
	for _, r := range out.Repos {
		proj[r.Path] = r.Project
	}
	a, b := proj[dir], proj[wt]
	if a == nil || b == nil || a.Key != b.Key || a.Label != filepath.Base(dir) {
		t.Fatalf("checkout + worktree = %+v / %+v, want one project named %s", a, b, filepath.Base(dir))
	}
	if o := proj[other]; o == nil || o.Key == a.Key {
		t.Errorf("an unrelated repo must be a project of its own, got %+v", o)
	}
}
