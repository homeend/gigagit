package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestAllowedSortMode(t *testing.T) {
	t.Parallel()
	for _, m := range []string{sortNameAsc, sortNameDesc, sortDateAsc, sortDateDesc, sortDefault} {
		if got := allowedSortMode(m); got != m {
			t.Errorf("allowedSortMode(%q) = %q", m, got)
		}
	}
	for _, junk := range []string{"", "name", "NAME-ASC", "date-asc; rm -rf /", "../../etc"} {
		if got := allowedSortMode(junk); got != sortDefault {
			t.Errorf("allowedSortMode(%q) = %q, want the default fallback", junk, got)
		}
	}
}

// sortRows mirrors the TUI's viewLess (internal/tui/viewstate.go): the compare
// is case-insensitive, the sort is stable, and an unknown date (0) sorts LAST
// in both directions so missing data never floats to the top.
func TestSortRowsMirrorsTUISemantics(t *testing.T) {
	t.Parallel()
	type row struct {
		name string
		date int64
	}
	name := func(r row) string { return r.name }
	date := func(r row) int64 { return r.date }
	base := []row{{"Beta", 300}, {"alpha", 100}, {"gamma", 0}, {"delta", 200}}

	cases := []struct {
		mode string
		want []string
	}{
		{sortDefault, []string{"Beta", "alpha", "gamma", "delta"}},
		{sortNameAsc, []string{"alpha", "Beta", "delta", "gamma"}},
		{sortNameDesc, []string{"gamma", "delta", "Beta", "alpha"}},
		{sortDateAsc, []string{"alpha", "delta", "Beta", "gamma"}},
		{sortDateDesc, []string{"Beta", "delta", "alpha", "gamma"}},
	}
	for _, c := range cases {
		rows := append([]row(nil), base...)
		sortRows(rows, c.mode, name, date)
		var got []string
		for _, r := range rows {
			got = append(got, r.name)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%s = %v, want %v", c.mode, got, c.want)
				break
			}
		}
	}
}

// Ties keep the input order (stable), like the TUI's sort.SliceStable.
func TestSortRowsIsStable(t *testing.T) {
	t.Parallel()
	type row struct{ name, tag string }
	rows := []row{{"a", "first"}, {"A", "second"}, {"a", "third"}}
	sortRows(rows, sortNameAsc, func(r row) string { return r.name }, func(row) int64 { return 0 })
	if rows[0].tag != "first" || rows[1].tag != "second" || rows[2].tag != "third" {
		t.Errorf("stable sort reordered equal keys: %v", rows)
	}
}

// The tags payload is capped at 100 rows. Sorting has to happen BEFORE that
// cap, or a sorted list is really "the server's arbitrary first hundred,
// sorted" — the wrong rows, not just the wrong order.
func TestTagsSortRunsBeforeTheCap(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	for i := 1; i <= maxTagRows+5; i++ {
		gitRun(t, dir, "tag", fmt.Sprintf("t%03d", i))
	}
	ts := serve(t, New(domain.Open(dir)))

	var got struct {
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
		Truncated bool `json:"truncated"`
	}
	if code := getJSON(t, ts, "/api/tags?sort="+sortNameDesc, &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(got.Tags) != maxTagRows || !got.Truncated {
		t.Fatalf("rows = %d, truncated = %v", len(got.Tags), got.Truncated)
	}
	if got.Tags[0].Name != fmt.Sprintf("t%03d", maxTagRows+5) {
		t.Errorf("first row = %q, want the LAST tag by name — the sort ran after the cap",
			got.Tags[0].Name)
	}
	for _, tg := range got.Tags {
		if tg.Name == "t001" {
			t.Error("t001 survived a name-desc window of 100 out of 105 tags")
		}
	}
}

func TestTagsSortAscending(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	for _, n := range []string{"v2.0", "v1.0", "v3.0"} {
		gitRun(t, dir, "tag", n)
	}
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	if code := getJSON(t, ts, "/api/tags?sort="+sortNameAsc, &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	want := []string{"v1.0", "v2.0", "v3.0"}
	for i, w := range want {
		if got.Tags[i].Name != w {
			t.Fatalf("tags = %+v, want %v", got.Tags, want)
		}
	}
}

// The domain hands every caller the SAME cached slice ("nobody mutates it" —
// tagsCached). Sorting it in place poisoned that cache: after one name-desc
// request, git's own order was gone for the rest of the process, in this
// frontend and every other one sharing the service.
func TestSortingDoesNotMutateTheSharedCache(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	for _, n := range []string{"v2.0", "v1.0", "v3.0"} {
		gitRun(t, dir, "tag", n)
	}
	ts := serve(t, New(domain.Open(dir)))

	names := func(path string) []string {
		var got struct {
			Tags []struct {
				Name string `json:"name"`
			} `json:"tags"`
		}
		if code := getJSON(t, ts, path, &got); code != http.StatusOK {
			t.Fatalf("GET %s: code = %d", path, code)
		}
		out := make([]string, 0, len(got.Tags))
		for _, tg := range got.Tags {
			out = append(out, tg.Name)
		}
		return out
	}

	first := names("/api/tags")
	names("/api/tags?sort=" + sortNameDesc) // must not disturb anything shared
	again := names("/api/tags")
	if len(first) != len(again) {
		t.Fatalf("row count changed: %v then %v", first, again)
	}
	for i := range first {
		if first[i] != again[i] {
			t.Fatalf("git order after a sorted request = %v, want the original %v", again, first)
		}
	}
}

func TestRemotesSortByName(t *testing.T) {
	t.Parallel()
	_, clone := cloneWithOrigin(t)
	// Two more remote-tracking branches, so ordering is observable.
	gitRun(t, clone, "branch", "aaa")
	gitRun(t, clone, "branch", "zzz")
	gitRun(t, clone, "push", "origin", "aaa", "zzz")
	ts := serve(t, New(domain.Open(clone)))

	var got struct {
		Remotes []struct {
			Name string `json:"name"`
			Time int64  `json:"time"`
		} `json:"remotes"`
	}
	if code := getJSON(t, ts, "/api/remotes?sort="+sortNameDesc, &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(got.Remotes) < 3 {
		t.Fatalf("remotes = %+v", got.Remotes)
	}
	if got.Remotes[0].Name != "origin/zzz" {
		t.Errorf("first = %q, want origin/zzz under name-desc", got.Remotes[0].Name)
	}
	if got.Remotes[len(got.Remotes)-1].Name != "origin/HEAD" && got.Remotes[len(got.Remotes)-1].Name != "origin/aaa" {
		t.Errorf("last = %q, want the alphabetically first row", got.Remotes[len(got.Remotes)-1].Name)
	}
}

// Worktree rows carry their HEAD's committer time so the client can sort them
// by date — the web's equivalent of the TUI's headTimes map.
func TestWorktreesCarryHeadTime(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, dir, "worktree", "add", "-b", "side", wt)
	if _, err := os.Stat(wt); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	var got struct {
		Worktrees []struct {
			Path string `json:"path"`
			Time int64  `json:"time"`
		} `json:"worktrees"`
	}
	if code := getJSON(t, ts, "/api/worktrees", &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(got.Worktrees) != 2 {
		t.Fatalf("worktrees = %+v", got.Worktrees)
	}
	for _, w := range got.Worktrees {
		if w.Time <= 0 {
			t.Errorf("worktree %s has time = %d, want its HEAD's committer time", w.Path, w.Time)
		}
	}
}
