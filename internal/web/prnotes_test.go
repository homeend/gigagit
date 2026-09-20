package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

func prThreads() []model.ForgeComment {
	t0 := time.Unix(1_700_000_000, 0)
	return []model.ForgeComment{
		{ID: "C1", Kind: model.ForgeCommentInline, Author: "carol", Path: "pr7.txt", Side: model.NoteSideNew,
			Line: 1, Body: "why this?\n\nplease explain", Created: t0, Updated: t0},
		{ID: "C2", ParentID: "C1", Kind: model.ForgeCommentInline, Author: "alice", Path: "pr7.txt", Side: model.NoteSideNew,
			Line: 1, Body: "will do", Created: t0, Updated: t0},
		{ID: "C3", Kind: model.ForgeCommentInline, Author: "dave", Path: "pr7.txt", Side: model.NoteSideNew,
			Line: 1, Body: "settled", Resolved: true, Created: t0, Updated: t0},
		{ID: "C4", Kind: model.ForgeCommentFile, Author: "erin", Path: "other.txt", Body: "whole file", Created: t0, Updated: t0},
		{ID: "C5", Kind: model.ForgeCommentInline, Author: "bob", Path: "pr7.txt", Outdated: true, Hunk: "@@ -1 +1 @@\n-old\n+new",
			Body: "stale remark", Created: t0, Updated: t0},
		{ID: "G1", Kind: model.ForgeCommentGeneral, Author: "bob", Body: "looks good overall", Created: t0, Updated: t0},
	}
}

type prNotesResp struct {
	Notes  []wireNote     `json:"notes"`
	Tip    string         `json:"tip"`
	Counts map[string]int `json:"counts"`
	Total  int            `json:"total"`
}

// prNotesServer is a fetched PR #7 with review threads on the fake forge.
func prNotesServer(t *testing.T) (*fakeForgeServer, string) {
	t.Helper()
	dir, bare, head := prFixture(t)
	pr := openPR(7, "Add a thing")
	pr.HeadSHA = head
	pr.Body = "the description"
	ff := &fakeForge{open: []model.PullRequest{pr}, baseURL: bare, comments: prThreads()}
	ts, _ := prServe(t, dir, ff)
	waitPRsLoaded(t, ts)
	if done := runPROp(t, ts, "pr-fetch", 7); done["ok"] != true {
		t.Fatalf("pr-fetch: %v", done)
	}
	return &fakeForgeServer{ts: ts, ff: ff}, head
}

func TestPRNotesServesCachedThreadsAndNeverCallsTheForge(t *testing.T) {
	fs, head := prNotesServer(t)
	var changed struct {
		Changed bool `json:"changed"`
	}
	if code := postJSON(t, fs.ts, "/api/pr/comments/refresh?n=7", "{}", "application/json", "", &changed); code != 200 || !changed.Changed {
		t.Fatalf("first refresh = %d %+v", code, changed)
	}
	before := fs.calls()
	var out prNotesResp
	for range 2 {
		if code := getJSON(t, fs.ts, "/api/pr/notes?n=7&path=pr7.txt", &out); code != 200 {
			t.Fatalf("notes = %d", code)
		}
	}
	if fs.calls() != before {
		t.Fatalf("a GET called the forge: %d → %d", before, fs.calls())
	}
	if out.Tip != head || len(out.Notes) != 2 {
		t.Fatalf("tip=%q notes=%+v", out.Tip, out.Notes)
	}
	root := out.Notes[0]
	if root.ID != "forge:C1" || !root.ReadOnly || root.Resolved || root.Summary != "why this?" || len(root.Replies) != 1 || root.Created == "" {
		t.Fatalf("root = %+v", root)
	}
	if !out.Notes[1].Resolved {
		t.Fatalf("the resolved thread lost its flag: %+v", out.Notes[1])
	}
	if out.Counts["pr7.txt"] != 2 || out.Counts["other.txt"] != 1 || out.Total != 3 {
		t.Fatalf("counts = %v total %d", out.Counts, out.Total)
	}
	var file prNotesResp
	getJSON(t, fs.ts, "/api/pr/notes?n=7&path=other.txt", &file)
	if len(file.Notes) != 1 || !file.Notes[0].FileLevel {
		t.Fatalf("file-level = %+v", file.Notes)
	}
}

func TestPRNotesCountsOnlyAndBeforeAnyRefresh(t *testing.T) {
	fs, _ := prNotesServer(t)
	var empty prNotesResp
	if code := getJSON(t, fs.ts, "/api/pr/notes?n=7&path=pr7.txt", &empty); code != 200 || len(empty.Notes) != 0 || empty.Total != 0 {
		t.Fatalf("before a refresh: %d %+v", code, empty)
	}
	postJSON(t, fs.ts, "/api/pr/comments/refresh?n=7", "{}", "application/json", "", nil)
	var counts prNotesResp
	if code := getJSON(t, fs.ts, "/api/pr/notes?n=7", &counts); code != 200 || counts.Notes == nil || len(counts.Notes) != 0 || counts.Total != 3 {
		t.Fatalf("counts-only: %d %+v", code, counts)
	}
}

func TestPRNotesRefusesBadInput(t *testing.T) {
	fs, _ := prNotesServer(t)
	for path, want := range map[string]int{
		"/api/pr/notes?n=0":               400,
		"/api/pr/notes?n=x":               400,
		"/api/pr/notes?n=99":              404,
		"/api/pr/notes?n=7&path=--output": 400,
	} {
		if code := getJSON(t, fs.ts, path, nil); code != want {
			t.Errorf("%s = %d, want %d", path, code, want)
		}
	}
}

func TestPRCommentsRefreshReportsChanged(t *testing.T) {
	fs, _ := prNotesServer(t)
	var r struct {
		Changed bool `json:"changed"`
	}
	post := func() bool {
		t.Helper()
		if code := postJSON(t, fs.ts, "/api/pr/comments/refresh?n=7", "{}", "application/json", "", &r); code != 200 {
			t.Fatalf("refresh = %d", code)
		}
		return r.Changed
	}
	if !post() || post() {
		t.Fatal("want changed on the first refresh only")
	}
	fs.ff.mu.Lock()
	fs.ff.comments = append(fs.ff.comments, model.ForgeComment{ID: "C9", Kind: model.ForgeCommentInline, Path: "pr7.txt", Line: 1, Body: "new"})
	fs.ff.mu.Unlock()
	if !post() {
		t.Fatal("a new thread must report changed")
	}
	if code := postJSON(t, fs.ts, "/api/pr/comments/refresh?n=99", "{}", "application/json", "", nil); code != 404 {
		t.Errorf("unknown PR = %d, want 404", code)
	}
}

func TestPRForgePostsAreWriteGuarded(t *testing.T) {
	t.Parallel()
	ts, _ := prServe(t, newRepoDir(t, 1), &fakeForge{})
	for _, path := range []string{"/api/pr/comments/refresh?n=7", "/api/pr/details?n=7"} {
		if code := postJSON(t, ts, path, "{}", "application/json", "https://evil.example", nil); code != 403 {
			t.Errorf("cross-origin %s = %d, want 403", path, code)
		}
		if code := postJSON(t, ts, path, "{}", "text/plain", "", nil); code != 415 {
			t.Errorf("non-JSON %s = %d, want 415", path, code)
		}
	}
	// R2: the details read spends forge calls, so it is never a GET.
	resp, err := http.Get(ts.URL + "/api/pr/details?n=7")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET details = %d, want 405", resp.StatusCode)
	}
}

func TestPRDetailsCarriesBodyHubAndOutdated(t *testing.T) {
	fs, _ := prNotesServer(t)
	var out struct {
		PR        model.PullRequest    `json:"pr"`
		Hub       []model.ForgeComment `json:"hub"`
		Outdated  []model.ForgeComment `json:"outdated"`
		Truncated bool                 `json:"truncated"`
	}
	if code := postJSON(t, fs.ts, "/api/pr/details?n=7", "{}", "application/json", "", &out); code != 200 {
		t.Fatalf("details = %d", code)
	}
	if out.PR.Number != 7 || out.PR.Body != "the description" {
		t.Fatalf("pr = %+v", out.PR)
	}
	if len(out.Hub) != 1 || out.Hub[0].Body != "looks good overall" {
		t.Fatalf("hub = %+v", out.Hub)
	}
	if len(out.Outdated) != 1 || out.Outdated[0].Hunk == "" {
		t.Fatalf("outdated = %+v", out.Outdated)
	}
	// The overlay's load also fills the note cache: the diff's threads follow.
	var notes prNotesResp
	getJSON(t, fs.ts, "/api/pr/notes?n=7&path=pr7.txt", &notes)
	if len(notes.Notes) != 2 {
		t.Fatalf("notes after details = %d", len(notes.Notes))
	}
}

func TestPROpenCarriesLinkPair(t *testing.T) {
	fs, _ := prNotesServer(t)
	var out struct {
		LinkSource string `json:"link_source"`
		LinkTarget string `json:"link_target"`
	}
	if code := getJSON(t, fs.ts, "/api/pr/open?n=7", &out); code != 200 {
		t.Fatalf("open = %d", code)
	}
	if out.LinkSource != "refs/gg/pr/7" || out.LinkTarget != "main" {
		t.Fatalf("link pair = %+v", out)
	}
}
