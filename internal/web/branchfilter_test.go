package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// bfRepo makes a repo with main (HEAD), feat/a, feat/old (backdated 200d),
// fix/b, and 130 remote-tracking branches origin/r000…r129 of which the
// even ones are feat/* — enough to prove the remotes filter runs before the
// 100-row cap.
func bfRepo(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t, 1)
	run := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		cmd.Env = append(cmd.Env, env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(nil, "branch", "feat/a")
	run(nil, "branch", "fix/b")
	old := time.Now().Add(-200 * 24 * time.Hour).Format(time.RFC3339)
	run([]string{"GIT_COMMITTER_DATE=" + old, "GIT_AUTHOR_DATE=" + old}, "commit", "--allow-empty", "-m", "old", "-q")
	run(nil, "branch", "feat/old")
	run(nil, "reset", "-q", "--hard", "HEAD~1")
	for i := 0; i < 130; i++ {
		name := fmt.Sprintf("r%03d", i)
		if i%2 == 0 {
			name = "feat/" + name
		}
		run(nil, "update-ref", "refs/remotes/origin/"+name, "HEAD")
	}
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(`
[[branches.filter]]
slot = 1
name = "feat"
prefix = "feat/"

[[branches.filter]]
slot = 2
name = "stale"
older_than = "90d"

[[branches.filter]]
slot = 4
regex = "("
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// bfServer serves bfRepo with a private state + config dir, and hands back
// the repo dir so a test can read the fixture's own refs.
func bfServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := bfRepo(t)
	ts := httptest.NewServer(New(domain.Open(dir)).Handler())
	t.Cleanup(ts.Close)
	return ts, dir
}

// allRemotes lists the fixture's remote-tracking refs straight from git, so
// the expectations follow the repo rather than a hardcoded count.
func allRemotes(t *testing.T, dir string) []string {
	t.Helper()
	out := gitRun(t, dir, "for-each-ref", "--format=%(refname:lstrip=2)", "refs/remotes")
	var names []string
	for _, ln := range strings.Split(out, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			names = append(names, ln)
		}
	}
	return names
}

type bfBranchesResp struct {
	Branches []struct {
		Name   string `json:"name"`
		IsHead bool   `json:"is_head"`
		Hidden bool   `json:"hidden"`
		Exempt bool   `json:"exempt"`
	} `json:"branches"`
	Filter *struct {
		Slot   int    `json:"slot"`
		Name   string `json:"name"`
		Mode   string `json:"mode"`
		Hidden int    `json:"hidden"`
	} `json:"filter"`
}

func TestBranchesCarryVerdictsUnderActiveSlot(t *testing.T) {
	ts, _ := bfServer(t)
	var out bfBranchesResp
	getJSON(t, ts, "/api/branches", &out)
	if out.Filter != nil {
		t.Fatalf("no slot yet: filter = %+v", out.Filter)
	}
	var put struct {
		Filter *struct{ Slot, Hidden int } `json:"filter"`
	}
	if code := putJSON(t, ts, "/api/branch-filter", `{"list":"branches","slot":1}`, "", &put); code != 200 {
		t.Fatalf("PUT: %d", code)
	}
	if put.Filter == nil || put.Filter.Slot != 1 {
		t.Fatalf("PUT filter = %+v", put.Filter)
	}
	getJSON(t, ts, "/api/branches", &out)
	if out.Filter == nil || out.Filter.Slot != 1 || out.Filter.Name != "feat" || out.Filter.Mode != "hide" || out.Filter.Hidden != 2 {
		t.Fatalf("filter = %+v", out.Filter)
	}
	byName := map[string]bool{}
	for _, b := range out.Branches {
		byName[b.Name] = b.Hidden
	}
	if !byName["feat/a"] || !byName["feat/old"] || byName["main"] || byName["fix/b"] {
		t.Errorf("hidden map = %v", byName)
	}
	if len(out.Branches) != 4 {
		t.Errorf("branches wire must keep hidden rows (flagged): %d rows", len(out.Branches))
	}
}

// TestBranchesExemptHeadUnderShowSlot proves the exemption rides the wire:
// a "show only fix/*" slot would hide main, but main is HEAD.
func TestBranchesExemptHeadUnderShowSlot(t *testing.T) {
	ts, dir := bfServer(t)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(`
[[branches.filter]]
slot = 1
name = "only fix"
mode = "show"
prefix = "fix/"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := putJSON(t, ts, "/api/branch-filter", `{"list":"branches","slot":1}`, "", nil); code != 200 {
		t.Fatalf("PUT: %d", code)
	}
	var out bfBranchesResp
	getJSON(t, ts, "/api/branches", &out)
	if out.Filter == nil || out.Filter.Mode != "show" || out.Filter.Hidden != 2 {
		t.Fatalf("filter = %+v; want show with 2 hidden (feat/a, feat/old)", out.Filter)
	}
	for _, b := range out.Branches {
		switch b.Name {
		case "main":
			if b.Hidden || !b.Exempt {
				t.Errorf("main: hidden=%v exempt=%v; HEAD must be exempt, never hidden", b.Hidden, b.Exempt)
			}
		case "fix/b":
			if b.Hidden || b.Exempt {
				t.Errorf("fix/b: hidden=%v exempt=%v; it matches the show rule", b.Hidden, b.Exempt)
			}
		default:
			if !b.Hidden {
				t.Errorf("%s: want hidden under show-only fix/", b.Name)
			}
		}
	}
}

func TestRemotesFilterBeforeCap(t *testing.T) {
	ts, dir := bfServer(t)
	var out struct {
		Remotes []struct {
			Name string `json:"name"`
		} `json:"remotes"`
		Truncated bool `json:"truncated"`
		Filter    *struct {
			Slot   int `json:"slot"`
			Hidden int `json:"hidden"`
		} `json:"filter"`
	}
	getJSON(t, ts, "/api/remotes", &out)
	if len(out.Remotes) != 100 || !out.Truncated {
		t.Fatalf("unfiltered: %d rows truncated=%v", len(out.Remotes), out.Truncated)
	}
	if out.Filter != nil {
		t.Fatalf("no slot yet: filter = %+v", out.Filter)
	}
	putJSON(t, ts, "/api/branch-filter", `{"list":"remotes","slot":1}`, "", nil)
	getJSON(t, ts, "/api/remotes", &out)
	// The fixture adds 65 feat/* and 65 plain remote branches; count what the
	// repo actually has rather than hardcoding.
	wantHidden, wantVisible := 0, 0
	for _, r := range allRemotes(t, dir) {
		if strings.HasPrefix(r, "origin/feat/") {
			wantHidden++
		} else {
			wantVisible++
		}
	}
	if wantVisible == 0 || wantHidden == 0 {
		t.Fatalf("fixture refs = %d hidden / %d visible", wantHidden, wantVisible)
	}
	if len(out.Remotes) != wantVisible || out.Truncated != (wantVisible > maxRemoteRows) {
		t.Errorf("filtered: %d rows truncated=%v; want %d (filter must run BEFORE the cap)", len(out.Remotes), out.Truncated, wantVisible)
	}
	if out.Filter == nil || out.Filter.Hidden != wantHidden {
		t.Errorf("filter = %+v; want hidden=%d", out.Filter, wantHidden)
	}
	for _, r := range out.Remotes {
		if strings.HasPrefix(r.Name, "origin/feat/") {
			t.Errorf("hidden remote row on the wire: %s", r.Name)
		}
	}
}

// TestBranchFilterRepoKeyCached pins the steady-state cost of the branch
// filter on the two live-refresh routes: the promptstate key is resolved with
// one `git rev-parse` and then served from the process-wide cache, and a
// re-root (a new *Service) never inherits the previous repo's key.
func TestBranchFilterRepoKeyCached(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := bfRepo(t)
	svc := domain.Open(dir)
	if got := cachedRepoKey(svc); got != "" {
		t.Fatalf("a fresh service starts with a cold key: %q", got)
	}
	ts := httptest.NewServer(New(svc).Handler())
	t.Cleanup(ts.Close)

	if code := putJSON(t, ts, "/api/branch-filter", `{"list":"branches","slot":1}`, "", nil); code != 200 {
		t.Fatalf("PUT: %d", code)
	}
	want, err := svc.GitCommonDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := cachedRepoKey(svc); got != want {
		t.Fatalf("cached key = %q; want %q (resolving it must warm the cache)", got, want)
	}
	// Both routes must still find the record now that they read the cache
	// instead of asking git — twice, so the warm path is the one under test.
	for i := 0; i < 2; i++ {
		var br bfBranchesResp
		getJSON(t, ts, "/api/branches", &br)
		if br.Filter == nil || br.Filter.Slot != 1 {
			t.Fatalf("GET %d /api/branches: filter = %+v", i, br.Filter)
		}
		var rm struct {
			Filter *struct{ Slot int } `json:"filter"`
		}
		getJSON(t, ts, "/api/remotes", &rm)
		if rm.Filter != nil {
			t.Fatalf("GET %d /api/remotes: remotes have their own slot: %+v", i, rm.Filter)
		}
	}
	if got := cachedRepoKey(domain.Open(dir)); got != "" {
		t.Errorf("a re-rooted service inherited the old key: %q", got)
	}
}

func TestBranchFilterPutValidation(t *testing.T) {
	ts, _ := bfServer(t)
	cases := []struct {
		body string
		code int
	}{
		{`{"list":"tags","slot":1}`, 400},
		{`{"list":"branches","slot":6}`, 400},
		{`{"list":"branches","slot":-1}`, 400},
		{`{"list":"branches","slot":4}`, 409}, // inert regex
		{`{"list":"branches","slot":3}`, 409}, // empty slot
		{`not json`, 400},
		{`{"list":"branches","slot":0}`, 200},
	}
	for _, c := range cases {
		if code := putJSON(t, ts, "/api/branch-filter", c.body, "", nil); code != c.code {
			t.Errorf("%s → %d; want %d", c.body, code, c.code)
		}
	}
}

func TestBranchFilterSlotsListing(t *testing.T) {
	ts, _ := bfServer(t)
	var out struct {
		Slots []struct {
			Slot    int    `json:"slot"`
			Name    string `json:"name"`
			Label   string `json:"label"`
			Mode    string `json:"mode"`
			Summary string `json:"summary"`
			Usable  bool   `json:"usable"`
			Error   string `json:"error"`
			Scope   string `json:"scope"`
			Prefix  string `json:"prefix"`
		} `json:"slots"`
		Warnings []string `json:"warnings"`
	}
	getJSON(t, ts, "/api/branch-filters", &out)
	if len(out.Slots) != 5 || out.Slots[0].Name != "feat" || !out.Slots[0].Usable || out.Slots[3].Usable || out.Slots[3].Error == "" || out.Slots[4].Usable {
		t.Fatalf("slots = %+v", out.Slots)
	}
	if out.Slots[0].Slot != 1 || out.Slots[0].Mode != "hide" || out.Slots[0].Prefix != "feat/" || out.Slots[0].Scope != "repo" {
		t.Errorf("slot 1 = %+v; want slot=1 mode=hide prefix=feat/ scope=repo", out.Slots[0])
	}
	if out.Slots[0].Summary == "" || out.Slots[4].Summary == "" {
		t.Errorf("every slot carries a summary: %+v", out.Slots)
	}
	if out.Slots[4].Name != "" || out.Slots[4].Label != "slot 5" || out.Slots[4].Scope != "" {
		t.Errorf("unset slot 5 = %+v; want an EMPTY name, the fallback label and no scope", out.Slots[4])
	}
	// The raw name and the display label are separate fields: the settings
	// form prefills from name, so a defined-but-unnamed slot must not hand it
	// "slot 4" to write back into the file.
	if out.Slots[0].Label != "feat" || out.Slots[3].Name != "" || out.Slots[3].Label != "slot 4" {
		t.Errorf("label vs name: slot 1 = %+v, slot 4 = %+v", out.Slots[0], out.Slots[3])
	}
	if out.Warnings == nil {
		t.Errorf("warnings must be [] on the wire, never null")
	}
}

// TestRemotesExemptUpstreamUnderShowSlot is the remotes twin of
// TestBranchesExemptHeadUnderShowSlot: a show-only rule that matches no
// remote branch would hide every row, but HEAD's upstream is exempt — and
// the remotes wire (which DROPS hidden rows rather than flagging them) must
// carry that survivor's exempt flag so the sidebar can mark it.
func TestRemotesExemptUpstreamUnderShowSlot(t *testing.T) {
	ts, dir := bfServer(t)
	gitRun(t, dir, "remote", "add", "origin", dir)
	gitRun(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitRun(t, dir, "branch", "--set-upstream-to=origin/main", "main")
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(`
[[branches.filter]]
slot = 1
name = "only fix"
mode = "show"
prefix = "fix/"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fixture check first, so a missing upstream cannot masquerade as a
	// handler bug.
	var br bfBranchesResp
	getJSON(t, ts, "/api/branches", &br)
	head := ""
	for _, b := range br.Branches {
		if b.IsHead {
			head = b.Name
		}
	}
	if head != "main" {
		t.Fatalf("fixture HEAD = %q; want main", head)
	}
	if code := putJSON(t, ts, "/api/branch-filter", `{"list":"remotes","slot":1}`, "", nil); code != 200 {
		t.Fatalf("PUT: %d", code)
	}
	var out struct {
		Remotes []struct {
			Name   string `json:"name"`
			Hidden bool   `json:"hidden"`
			Exempt bool   `json:"exempt"`
		} `json:"remotes"`
		Filter *struct{ Hidden int } `json:"filter"`
	}
	getJSON(t, ts, "/api/remotes", &out)
	if len(out.Remotes) != 1 || out.Remotes[0].Name != "origin/main" {
		t.Fatalf("remotes = %+v; want only the exempt upstream row", out.Remotes)
	}
	if !out.Remotes[0].Exempt || out.Remotes[0].Hidden {
		t.Errorf("origin/main: exempt=%v hidden=%v; the upstream row survives, flagged", out.Remotes[0].Exempt, out.Remotes[0].Hidden)
	}
	if out.Filter == nil || out.Filter.Hidden != len(allRemotes(t, dir))-1 {
		t.Errorf("filter = %+v; want every other remote hidden", out.Filter)
	}
}
