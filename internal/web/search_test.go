package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// feedResp is the /api/commits payload the filter tests read.
type feedResp struct {
	Rows []struct {
		Hash    string `json:"hash"`
		Short   string `json:"short"`
		Subject string `json:"subject"`
		Author  string `json:"author"`
		Cells   string `json:"cells"`
	} `json:"rows"`
	CanLoadMore bool   `json:"can_load_more"`
	Solo        string `json:"solo"`
	SoloKind    string `json:"solo_kind"`
	Filtered    bool   `json:"filtered"`
}

func (f feedResp) subjects() []string {
	out := make([]string, len(f.Rows))
	for i, r := range f.Rows {
		out[i] = r.Subject
	}
	return out
}

// commitAs commits everything staged in dir with an explicit author AND an
// explicit date, so the author and date filters have something stable to
// select on.
//
// It shells out directly rather than through gitRun because the date is
// ENVIRONMENT, not an argument: `git commit --date` sets only the author's
// half, and `git log --since/--until` read the committer's. Dates in the
// 2020s are also deliberate — git's approxidate answers `--since=1970-01-01`
// with nothing and `--until=1970-01-01` with everything, so an epoch fixture
// tests the opposite of what it reads like.
func commitAs(t *testing.T, dir, author, date, msg string) {
	t.Helper()
	cmd := exec.Command("git", "-c", "commit.gpgsign=false", "commit", "--author="+author, "-m", msg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit %s: %v\n%s", msg, err, out)
	}
}

// filterRepo builds a repo with two authors, two paths and four distinct
// dates on main plus a side branch — everything the feed filters need to be
// told apart:
//
//	main:  c-a1 (alice, a.txt, 2020) → c-b1 (bob, b.txt, 2021) → c-a2 (alice, a.txt, 2022)
//	side:  c-side (bob, s.txt, 2023) branched off c-a1
func filterRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "-A")
	}
	write("a.txt", "a1\n")
	commitAs(t, dir, "Alice <alice@example.com>", "2020-01-01T12:00:00+0000", "c-a1")
	first := gitRun(t, dir, "rev-parse", "HEAD")
	write("b.txt", "b1\n")
	commitAs(t, dir, "Bob <bob@example.com>", "2021-01-01T12:00:00+0000", "c-b1")
	write("a.txt", "a2\n")
	commitAs(t, dir, "Alice <alice@example.com>", "2022-01-01T12:00:00+0000", "c-a2")
	gitRun(t, dir, "checkout", "-q", "-b", "side", first)
	write("s.txt", "s\n")
	commitAs(t, dir, "Bob <bob@example.com>", "2023-01-01T12:00:00+0000", "c-side")
	gitRun(t, dir, "checkout", "-q", "main")
	return dir
}

func TestFeedFilterNarrows(t *testing.T) {
	t.Parallel()
	dir := filterRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{"unfiltered", "", []string{"c-side", "c-a2", "c-b1", "c-a1"}},
		{"author", "?author=Alice", []string{"c-a2", "c-a1"}},
		{"path", "?path=b.txt", []string{"c-b1"}},
		{"message", "?grep=side", []string{"c-side"}},
		{"since", "?since=2021-06-01", []string{"c-side", "c-a2"}},
		{"until", "?until=2020-06-01", []string{"c-a1"}},
		{"a date window", "?since=2020-06-01&until=2021-06-01", []string{"c-b1"}},
		{"author and path together", "?author=Alice&path=a.txt", []string{"c-a2", "c-a1"}},
		// A key this build does not know is not an error: the browser and the
		// server ship together but are not necessarily loaded together.
		{"unknown key ignored", "?nosuchfilter=1", []string{"c-side", "c-a2", "c-b1", "c-a1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got feedResp
			if code := getJSON(t, ts, "/api/commits"+tc.query, &got); code != http.StatusOK {
				t.Fatalf("status = %d, want 200", code)
			}
			// Compared as a SET: what is under test is WHICH commits a
			// filter selects, not how the walk orders two tips against
			// each other (TestCommitsEndpoint pins newest-first).
			gotS, wantS := append([]string(nil), got.subjects()...), append([]string(nil), tc.want...)
			sort.Strings(gotS)
			sort.Strings(wantS)
			if strings.Join(gotS, ",") != strings.Join(wantS, ",") {
				t.Fatalf("subjects = [%s], want [%s]", strings.Join(got.subjects(), ","), strings.Join(tc.want, ","))
			}
		})
	}
}

// A filtered page is a subset of history, so the server computes no lanes for
// it — that cost is the reason to skip it on a big repo, and the lanes would
// be wrong anyway.
func TestFeedFilterDropsTheGraph(t *testing.T) {
	t.Parallel()
	dir := filterRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	var plain feedResp
	getJSON(t, ts, "/api/commits", &plain)
	if !strings.Contains(plain.Rows[0].Cells, "●") {
		t.Fatalf("unfiltered cells = %q, want a node glyph", plain.Rows[0].Cells)
	}
	if plain.Filtered {
		t.Error("filtered = true with no filter")
	}

	var filtered feedResp
	getJSON(t, ts, "/api/commits?author=Alice", &filtered)
	if !filtered.Filtered {
		t.Error("filtered = false with an author filter")
	}
	for _, r := range filtered.Rows {
		if r.Cells != "" {
			t.Errorf("filtered row %s has lanes %q, want none", r.Short, r.Cells)
		}
	}
}

// Clearing the filter restores the whole list — the same feed, re-scoped, not
// a different one.
func TestFeedFilterClears(t *testing.T) {
	t.Parallel()
	dir := filterRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	var narrowed feedResp
	getJSON(t, ts, "/api/commits?author=Alice", &narrowed)
	if len(narrowed.Rows) != 2 {
		t.Fatalf("filtered rows = %d, want 2", len(narrowed.Rows))
	}
	var restored feedResp
	getJSON(t, ts, "/api/commits", &restored)
	if len(restored.Rows) != 4 {
		t.Fatalf("rows after clearing = %d, want 4", len(restored.Rows))
	}
	if restored.Filtered {
		t.Error("filtered = true after clearing")
	}
	if !strings.Contains(restored.Rows[0].Cells, "●") {
		t.Error("lanes did not come back after clearing the filter")
	}
}

// A filter composes with the branch scope rather than replacing it.
func TestFeedFilterComposesWithSolo(t *testing.T) {
	t.Parallel()
	dir := filterRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/solo", `{"branch":"main"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("solo status = %d", code)
	}
	var got feedResp
	getJSON(t, ts, "/api/commits?author=Bob", &got)
	// c-side is Bob's too, but it is not on main: the branch scope still holds.
	if want := "c-b1"; strings.Join(got.subjects(), ",") != want {
		t.Fatalf("subjects = %v, want [%s]", got.subjects(), want)
	}
	if got.Solo != "main" || got.SoloKind != "branch" {
		t.Errorf("solo = %q/%q, want main/branch", got.Solo, got.SoloKind)
	}
}

// A filter must survive the feed being dropped (which every state-changing op
// does): the request carries it, so the rebuilt feed starts under it.
func TestFeedFilterSurvivesReset(t *testing.T) {
	t.Parallel()
	dir := filterRepo(t)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	getJSON(t, ts, "/api/commits?author=Alice", nil)
	srv.resetFeed()
	var got feedResp
	getJSON(t, ts, "/api/commits?author=Alice", &got)
	if len(got.Rows) != 2 || !got.Filtered {
		t.Fatalf("after resetFeed: rows = %v, filtered = %v; want Alice's 2", got.subjects(), got.Filtered)
	}
	// …and the same through the explicit reset=1 (the manual refresh).
	var reset feedResp
	getJSON(t, ts, "/api/commits?author=Alice&reset=1", &reset)
	if len(reset.Rows) != 2 {
		t.Fatalf("reset=1 rows = %v, want Alice's 2", reset.subjects())
	}
}

// Control characters are refused: domain's scope cache key joins the filter
// fields with NUL, so a value carrying one could collide two different filters
// onto one cached page.
func TestFeedFilterRejectsControlCharacters(t *testing.T) {
	t.Parallel()
	dir := filterRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	for _, q := range []string{
		"/api/commits?author=" + url.QueryEscape("a\x00b"),
		"/api/commits?grep=" + url.QueryEscape("a\nb"),
		"/api/commits?path=" + url.QueryEscape("a\x1fb"),
	} {
		if code := getJSON(t, ts, q, nil); code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", q, code)
		}
	}
}

// A filter value that looks like a flag is still just a value: it reaches git
// as one argv entry (and a path after `--`), so it narrows to nothing rather
// than being read as an option.
func TestFeedFilterValuesAreNotFlags(t *testing.T) {
	t.Parallel()
	dir := filterRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	var got feedResp
	if code := getJSON(t, ts, "/api/commits?author="+url.QueryEscape("--all"), &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(got.Rows) != 0 {
		t.Fatalf("rows = %v, want none (no author named --all)", got.subjects())
	}
}

// --- solo from a commit -----------------------------------------------------

func TestSoloFromCommit(t *testing.T) {
	t.Parallel()
	dir := filterRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	sha := shaOf(t, dir, "c-b1")

	var set struct {
		Solo string `json:"solo"`
		Kind string `json:"solo_kind"`
	}
	if code := postJSON(t, ts, "/api/solo", `{"kind":"commit","ref":"`+sha+`"}`, "application/json", "", &set); code != http.StatusOK {
		t.Fatalf("solo status = %d", code)
	}
	if set.Solo != sha || set.Kind != "commit" {
		t.Fatalf("stored solo = %q/%q, want %q/commit", set.Solo, set.Kind, sha)
	}
	var got feedResp
	getJSON(t, ts, "/api/commits", &got)
	// Only c-b1's ancestry: c-a2 is newer, c-side is on another branch.
	if want := "c-b1,c-a1"; strings.Join(got.subjects(), ",") != want {
		t.Fatalf("subjects = %v, want [%s]", got.subjects(), want)
	}
	if got.Solo != sha || got.SoloKind != "commit" {
		t.Errorf("reported solo = %q/%q, want %q/commit", got.Solo, got.SoloKind, sha)
	}
}

// A short (abbreviated) commit id is stored as the full hash, so the walk can
// never hit a short-sha ambiguity.
func TestSoloFromCommitStoresFullSha(t *testing.T) {
	t.Parallel()
	dir := filterRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	sha := shaOf(t, dir, "c-b1")

	var set struct {
		Solo string `json:"solo"`
	}
	if code := postJSON(t, ts, "/api/solo", `{"kind":"commit","ref":"`+sha[:8]+`"}`, "application/json", "", &set); code != http.StatusOK {
		t.Fatalf("solo status = %d", code)
	}
	if set.Solo != sha {
		t.Fatalf("stored solo = %q, want the full %q", set.Solo, sha)
	}
}

// The branch form keeps behaving exactly as it did, including its refusals.
func TestSoloBranchUnchanged(t *testing.T) {
	t.Parallel()
	dir := filterRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	var got feedResp
	postJSON(t, ts, "/api/solo", `{"branch":"side"}`, "application/json", "", nil)
	getJSON(t, ts, "/api/commits", &got)
	if want := "c-side,c-a1"; strings.Join(got.subjects(), ",") != want {
		t.Fatalf("subjects = %v, want [%s]", got.subjects(), want)
	}
	// …and clearing it shows everything again.
	postJSON(t, ts, "/api/solo", `{"branch":""}`, "application/json", "", nil)
	getJSON(t, ts, "/api/commits", &got)
	if len(got.Rows) != 4 || got.Solo != "" || got.SoloKind != "" {
		t.Fatalf("after clearing: %v solo=%q/%q", got.subjects(), got.Solo, got.SoloKind)
	}
}

func TestSoloGuards(t *testing.T) {
	t.Parallel()
	dir := filterRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"branch":"nope"}`, http.StatusNotFound},
		{`{"kind":"commit","ref":"nothex"}`, http.StatusBadRequest},
		{`{"kind":"commit","ref":"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}`, http.StatusNotFound},
		// A real object that is not a commit: the resolve peels with ^{commit},
		// so a tree sha never enters a scope the feed would then fail to walk.
		{`{"kind":"commit","ref":"` + gitRun(t, dir, "rev-parse", "HEAD^{tree}") + `"}`, http.StatusNotFound},
		{`{"kind":"nonsense","ref":"main"}`, http.StatusBadRequest},
		{`{"kind":"branch","ref":"--upload-pack=x"}`, http.StatusBadRequest},
	} {
		if code := postJSON(t, ts, "/api/solo", tc.body, "application/json", "", nil); code != tc.want {
			t.Errorf("solo %s = %d, want %d", tc.body, code, tc.want)
		}
	}
}

// --- the fuzzy file finder --------------------------------------------------

type filesResp struct {
	Files   []string `json:"files"`
	Total   int      `json:"total"`
	Limited bool     `json:"limited"`
}

func TestFilesEndpointRanks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	for _, p := range []string{"cmd/gg/main.go", "internal/web/search.go", "docs/readme.md"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c1")
	ts := serve(t, New(domain.Open(dir)))

	var all filesResp
	if code := getJSON(t, ts, "/api/files", &all); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if all.Total != 3 || len(all.Files) != 3 {
		t.Fatalf("files = %v (total %d), want 3", all.Files, all.Total)
	}

	var ranked filesResp
	getJSON(t, ts, "/api/files?q=search", &ranked)
	if len(ranked.Files) != 1 || ranked.Files[0] != "internal/web/search.go" {
		t.Fatalf("ranked = %v, want [internal/web/search.go]", ranked.Files)
	}
	if ranked.Total != 3 {
		t.Errorf("total = %d, want the whole tracked count 3", ranked.Total)
	}

	// A subsequence match, not a substring one: the finder's whole point.
	var fuzzyGot filesResp
	getJSON(t, ts, "/api/files?q=cgmain", &fuzzyGot)
	if len(fuzzyGot.Files) != 1 || fuzzyGot.Files[0] != "cmd/gg/main.go" {
		t.Fatalf("fuzzy = %v, want [cmd/gg/main.go]", fuzzyGot.Files)
	}

	var none filesResp
	getJSON(t, ts, "/api/files?q=zzzznope", &none)
	if len(none.Files) != 0 {
		t.Fatalf("files = %v, want none", none.Files)
	}
}

func TestFilesEndpointLimits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	for i := 0; i < 60; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c1")
	ts := serve(t, New(domain.Open(dir)))

	var got filesResp
	getJSON(t, ts, "/api/files?q=f", &got)
	if len(got.Files) != fileFinderLimit || !got.Limited {
		t.Fatalf("files = %d (limited %v), want %d and limited", len(got.Files), got.Limited, fileFinderLimit)
	}
	if got.Total != 60 {
		t.Errorf("total = %d, want 60", got.Total)
	}
}

// The path list is read once per HEAD: a new commit invalidates it, so a file
// added by that commit shows up without restarting anything.
func TestFilesCacheFollowsHead(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	var before filesResp
	getJSON(t, ts, "/api/files", &before)
	if before.Total != 1 {
		t.Fatalf("total = %d, want 1", before.Total)
	}
	st := srv.searchState()
	if st.filesHead == "" || len(st.files) != 1 {
		t.Fatalf("cache = %q/%v, want the HEAD read", st.filesHead, st.files)
	}

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c2")

	var after filesResp
	getJSON(t, ts, "/api/files", &after)
	if after.Total != 2 {
		t.Fatalf("total = %d after a commit, want 2 (the cache did not follow HEAD)", after.Total)
	}
}

// --- squashing marked commits ----------------------------------------------

func startSquash(t *testing.T, ts *httptest.Server, shas ...string) (string, int) {
	t.Helper()
	quoted := make([]string, len(shas))
	for i, s := range shas {
		quoted[i] = `"` + s + `"`
	}
	var out struct {
		OpID string `json:"op_id"`
	}
	code := postJSON(t, ts, "/api/commit-squash", `{"shas":[`+strings.Join(quoted, ",")+`]}`, "application/json", "", &out)
	return out.OpID, code
}

func TestSquashMarkedCommits(t *testing.T) {
	t.Parallel()
	useGGSequenceEditor(t)
	dir := editRepo(t, 4) // c1 a.txt, c2 b.txt, c3 c.txt, c4 d.txt
	ts := serve(t, New(domain.Open(dir)))

	// Marks arrive newest-first, the order the feed holds them in.
	opID, code := startSquash(t, ts, shaOf(t, dir, "c3"), shaOf(t, dir, "c2"))
	if code != http.StatusAccepted {
		t.Fatalf("squash status = %d, want 202", code)
	}
	// readSSE returns only on the terminal done event, so by the time it
	// returns the op has finished (the feed is never consulted here; subjects
	// reads git directly). What the old form dropped was the done event's
	// VERDICT: a squash whose rebase fails before it starts — git aborts
	// cleanly when the sequence editor cannot run, leaving history untouched
	// and no rebase in progress — still ends in a done event, just with
	// ok=false. Reading only the log then reported the misleading "subjects =
	// [c4 c3 c2 c1]" instead of the op's own error. That is exactly how the
	// pre-e450bee7 useGGSequenceEditor failed under a parallel run: each
	// caller restored the execPath global in its Cleanup, so a sibling
	// finishing first put os.Executable (the test binary) back while this
	// test's buildSquash was still to read it, and git ran `web.test
	// __rebase-seq …` as the editor. Asserting the verdict first keeps any
	// future failure of that class self-describing.
	events := readSSE(t, ts, opID, 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true || done["changed"] != true {
		t.Fatalf("squash op did not succeed: done = %v", done)
	}

	got := subjects(t, dir)
	if len(got) != 3 {
		t.Fatalf("subjects = %v, want 3 commits after squashing two", got)
	}
	if got[0] != "c4" || got[2] != "c1" {
		t.Fatalf("subjects = %v, want c4 … c1 around the squashed commit", got)
	}
	// The squashed commit carries both messages (git's squash default).
	msg := gitRun(t, dir, "log", "-1", "--format=%B", "HEAD~1")
	if !strings.Contains(msg, "c2") || !strings.Contains(msg, "c3") {
		t.Errorf("squashed message = %q, want both c2 and c3", msg)
	}
	// Every file survives: a squash folds commits, it does not drop changes.
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s missing after the squash: %v", name, err)
		}
	}
}

func TestSquashRefusals(t *testing.T) {
	t.Parallel()
	useGGSequenceEditor(t)
	dir := editRepo(t, 4)
	ts := serve(t, New(domain.Open(dir)))
	gitRun(t, dir, "checkout", "-q", "-b", "other", "HEAD~3")
	offBranch := gitRun(t, dir, "rev-parse", "HEAD") // c1, the only shared commit
	gitRun(t, dir, "checkout", "-q", "main")

	for _, tc := range []struct {
		name string
		shas []string
		want int
	}{
		{"one commit is not a squash", []string{shaOf(t, dir, "c3")}, http.StatusUnprocessableEntity},
		{"a duplicate does not make two", []string{shaOf(t, dir, "c3"), shaOf(t, dir, "c3")}, http.StatusUnprocessableEntity},
		{"not a commit id", []string{shaOf(t, dir, "c3"), "main"}, http.StatusBadRequest},
		// c4 and c2 are not adjacent — c3 sits between them.
		{"not adjacent", []string{shaOf(t, dir, "c4"), shaOf(t, dir, "c2")}, http.StatusUnprocessableEntity},
		// A commit that is not in the onto..branch range fails membership.
		{"not on this branch", []string{shaOf(t, dir, "c3"), offBranch}, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, code := startSquash(t, ts, tc.shas...)
			if code != tc.want {
				t.Fatalf("status = %d, want %d", code, tc.want)
			}
			if s := subjects(t, dir); len(s) != 4 {
				t.Fatalf("history changed on a refusal: %v", s)
			}
		})
	}
}

// The squash endpoint mutates, so it lives behind the same write guard as
// every other mutation: a cross-origin post cannot start one.
func TestSquashWriteGuard(t *testing.T) {
	t.Parallel()
	dir := editRepo(t, 3)
	ts := serve(t, New(domain.Open(dir)))
	body := `{"shas":["` + shaOf(t, dir, "c3") + `","` + shaOf(t, dir, "c2") + `"]}`
	if code := postJSON(t, ts, "/api/commit-squash", body, "application/json", "http://evil.example", nil); code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want 403", code)
	}
	if code := postJSON(t, ts, "/api/commit-squash", body, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Fatalf("bad content-type status = %d, want 415", code)
	}
}
