package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// A stored overview — a review's or a commit's permanent one — is an entry
// of the shared open-files list once a tab opens it (spec
// 2026-10-29-web-stored-overview-tree): the TUI's srcReviewOverview /
// srcCommitOverview open files.

type storedRowWire struct {
	steer.OpenFile
	Stored *struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
		Tip  string `json:"tip"`
	} `json:"stored"`
	Children []struct {
		Path   string `json:"path"`
		Anchor int    `json:"anchor"`
		Rev    string `json:"rev"`
	} `json:"children"`
}

func pageRows(t *testing.T, ts *httptest.Server) []storedRowWire {
	t.Helper()
	var body struct {
		Files []storedRowWire `json:"files"`
	}
	if code := getJSON(t, ts, "/api/open-files", &body); code != http.StatusOK {
		t.Fatalf("GET open-files = %d", code)
	}
	return body.Files
}

func storedRow(t *testing.T, ts *httptest.Server, id string) (storedRowWire, bool) {
	t.Helper()
	for _, f := range pageRows(t, ts) {
		if f.ID == id {
			return f, true
		}
	}
	return storedRowWire{}, false
}

func registerStored(t *testing.T, ts *httptest.Server, kind, id, tab string) (int, ofAns) {
	t.Helper()
	return postOpenFiles(t, ts, `{"op":"stored","kind":"`+kind+`","id":"`+id+`","tab":"`+tab+`"}`)
}

func TestStoredRegisterListsIt(t *testing.T) {
	t.Parallel()
	ts, svc, id := overviewServer(t)
	code, a := registerStored(t, ts, "overview", id, "t1")
	if code != http.StatusOK || a.File == nil {
		t.Fatalf("register = %d %+v", code, a)
	}
	head, _, _ := svc.ResolveRev(context.Background(), "HEAD")
	head = strings.TrimSpace(head)
	f, ok := storedRow(t, ts, a.File.ID)
	if !ok {
		t.Fatalf("not listed: %+v", pageRows(t, ts))
	}
	if f.Source != "overview" || f.Title != "Overview: How f works" || f.Anchors != 1 || f.State != "shown" {
		t.Fatalf("row = %+v", f.OpenFile)
	}
	if f.Stored == nil || f.Stored.Kind != "overview" || f.Stored.ID != id || f.Stored.Tip != head {
		t.Fatalf("stored = %+v", f.Stored)
	}
	if len(f.Children) != 1 || f.Children[0].Path != "f.txt" || f.Children[0].Anchor != 0 || f.Children[0].Rev != head {
		t.Fatalf("children = %+v", f.Children)
	}
	// Opened again: the same entry, not a second one.
	if _, b := registerStored(t, ts, "overview", id, "t1"); b.File == nil || b.File.ID != a.File.ID || len(pageRows(t, ts)) != 1 {
		t.Fatalf("re-register = %+v rows %d", b.File, len(pageRows(t, ts)))
	}
}

func TestStoredReviewOverview(t *testing.T) {
	t.Parallel()
	ts, _, sha, id := reviewServer(t, webReviewDocWithOverview)
	code, a := registerStored(t, ts, "review", id, "t1")
	if code != http.StatusOK || a.File == nil {
		t.Fatalf("register = %d %+v", code, a)
	}
	f, _ := storedRow(t, ts, a.File.ID)
	if f.Stored == nil || f.Stored.Kind != "review" || f.Stored.Tip != sha || !strings.HasPrefix(f.Title, "Overview: ") || f.Anchors != 3 {
		t.Fatalf("row = %+v stored %+v", f.OpenFile, f.Stored)
	}
	// Path anchors only (a note: anchor names nothing), first-named order.
	if len(f.Children) != 2 || f.Children[0].Path != "f.txt" || f.Children[1].Path != "nope.go" || f.Children[1].Anchor != 1 || f.Children[0].Rev != sha {
		t.Fatalf("children = %+v", f.Children)
	}
}

func TestStoredWorkingReviewChildrenHaveNoRev(t *testing.T) {
	t.Parallel()
	ts, svc, _, _ := workingReviewServer(t)
	doc := `{"version":1,"summary":"w","files":[{"path":"w.txt","annotations":[{"newRange":[1,1],"summary":"A"}]}],"overview":"See [w](w.txt:1)."}`
	id, _, err := svc.SaveReview(context.Background(), domain.SaveReview{Target: domain.WorkingReviewTarget(), Agent: "Claude", Text: doc,
		Files: []model.NoteFile{{Path: "w.txt", Blob: sha1Blob("hello\n")}}})
	if err != nil {
		t.Fatal(err)
	}
	_, a := registerStored(t, ts, "review", id, "t1")
	if a.File == nil {
		t.Fatal("not registered")
	}
	f, _ := storedRow(t, ts, a.File.ID)
	if f.Stored == nil || f.Stored.Tip != "" || len(f.Children) != 1 || f.Children[0].Rev != "" {
		t.Fatalf("stored %+v children %+v", f.Stored, f.Children)
	}
}

func TestStoredSurvivesFollowDocs(t *testing.T) {
	t.Parallel()
	_, svc, id := overviewServer(t)
	srv := New(svc)
	ts := serve(t, srv)
	_, a := registerStored(t, ts, "overview", id, "t1")
	if a.File == nil {
		t.Fatal("not registered")
	}
	srv.followDocs()
	if _, ok := storedRow(t, ts, a.File.ID); !ok {
		t.Fatal("a follow pass dropped the stored overview")
	}
	if _, ok := srv.ofs.overviewByID(svc.Root(), a.File.ID); ok {
		t.Fatal("a stored overview must never read as a session overview")
	}
}

func TestStoredNeverEvicted(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(agentdocs.New().NextFileSeq)
	st := r.openStored("/wt", ofKey{Src: "commit-overview", Rev: "ov1", Path: "overview-ov1.md"}, ofStored{Kind: "overview", ID: "ov1"}, "Overview: x", "")
	for i := 0; i < maxOpenFiles; i++ {
		r.open("/wt", wtKey(fmt.Sprintf("p%d.txt", i)), "", 0)
	}
	if _, e := r.findLocked("/wt", st.ID); e == nil {
		t.Fatal("the cap evicted a stored overview")
	}
}

func TestStoredCloseRemoves(t *testing.T) {
	t.Parallel()
	ts, _, id := overviewServer(t)
	_, a := registerStored(t, ts, "overview", id, "t1")
	if code, _ := postOpenFiles(t, ts, `{"op":"close","id":"`+a.File.ID+`","tab":"t1"}`); code != http.StatusOK {
		t.Fatalf("close = %d", code)
	}
	if _, ok := storedRow(t, ts, a.File.ID); ok {
		t.Fatal("esc on a stored overview must close it")
	}
}

func TestStoredBackgroundThenCloseKeeps(t *testing.T) {
	t.Parallel()
	ts, _, id := overviewServer(t)
	_, a := registerStored(t, ts, "overview", id, "t1")
	postOpenFiles(t, ts, `{"op":"background","id":"`+a.File.ID+`","tab":"t1"}`)
	postOpenFiles(t, ts, `{"op":"focus","id":"`+a.File.ID+`","tab":"t1"}`)
	postOpenFiles(t, ts, `{"op":"close","id":"`+a.File.ID+`","tab":"t1"}`)
	f, ok := storedRow(t, ts, a.File.ID)
	if !ok || f.State != "background" {
		t.Fatalf("after ctrl+] esc backgrounds it (the TUI's escDoc): %v %+v", ok, f.OpenFile)
	}
}

func TestStoredEverywhereCloseRemoves(t *testing.T) {
	t.Parallel()
	ts, _, id := overviewServer(t)
	_, a := registerStored(t, ts, "overview", id, "t1")
	postOpenFiles(t, ts, `{"op":"background","id":"`+a.File.ID+`","tab":"t1"}`)
	postOpenFiles(t, ts, `{"op":"close","id":"`+a.File.ID+`","tab":"t2","everywhere":true}`)
	if _, ok := storedRow(t, ts, a.File.ID); ok {
		t.Fatal("x closes a stored overview")
	}
}

func TestStoredGoneIs404(t *testing.T) {
	t.Parallel()
	ts, _, _ := overviewServer(t)
	if code, _ := registerStored(t, ts, "overview", "20990101T000000-dead", "t1"); code != http.StatusNotFound {
		t.Fatalf("gone overview = %d, want 404", code)
	}
	if len(pageRows(t, ts)) != 0 {
		t.Fatal("a gone overview was listed")
	}
}

func TestStoredBadKindIs400(t *testing.T) {
	t.Parallel()
	ts, _, id := overviewServer(t)
	for _, body := range []string{
		`{"op":"stored","kind":"session","id":"` + id + `"}`,
		`{"op":"stored","kind":"overview","id":"../x"}`,
		`{"op":"stored","kind":"review","id":""}`,
	} {
		if code, _ := postOpenFiles(t, ts, body); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", body, code)
		}
	}
}

// A permanent overview deleted behind the page leaves the list on the next
// counts read (the TUI's dropGoneOverviews); a failed read keeps it.
func TestStoredGonePermanentLeavesOnCounts(t *testing.T) {
	t.Parallel()
	ts, svc, id := overviewServer(t)
	_, a := registerStored(t, ts, "overview", id, "t1")
	postOpenFiles(t, ts, `{"op":"background","id":"`+a.File.ID+`","tab":"t1"}`)
	var counts map[string]any
	getJSON(t, ts, "/api/notes/counts", &counts)
	if _, ok := storedRow(t, ts, a.File.ID); !ok {
		t.Fatal("a counts read dropped a live overview")
	}
	if err := svc.RemoveOverview(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	getJSON(t, ts, "/api/notes/counts", &counts)
	if _, ok := storedRow(t, ts, a.File.ID); ok {
		t.Fatal("a deleted permanent overview stayed listed")
	}
}

// Review I1: the list update that drops a deleted permanent overview names
// it (closed), so a tab showing it says "was deleted", not "closed in
// another tab".
func TestStoredGoneBroadcastNamesIt(t *testing.T) {
	isolateGlobal(t)
	_, svc, id := overviewServer(t)
	srv := New(svc)
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	next, stop := eventsFor(t, ts, "t9")
	defer stop()
	_, a := registerStored(t, ts, "overview", id, "t1")
	if err := svc.RemoveOverview(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	var counts map[string]any
	getJSON(t, ts, "/api/notes/counts", &counts)
	for i := 0; i < 10; i++ {
		m := next()
		if m.Reason == "open_files" && len(m.Closed) > 0 {
			if m.Closed[0] != a.File.ID {
				t.Fatalf("closed = %v, want %s", m.Closed, a.File.ID)
			}
			return
		}
	}
	t.Fatal("no open_files message named the dropped overview")
}

// Review I3: listing a stored overview reads its document only — no file
// of the commit (each anchored path's lines) is read to count its anchors.
func TestStoredRegisterReadsNoFile(t *testing.T) {
	ts, _, id := overviewServer(t)
	rts, _, _, rid := reviewServer(t, webReviewDocWithOverview)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\necho \"$*\" >> "+log+"\nexec "+real+" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if code, _ := registerStored(t, ts, "overview", id, "t1"); code != http.StatusOK {
		t.Fatalf("register overview = %d", code)
	}
	if code, _ := registerStored(t, rts, "review", rid, "t1"); code != http.StatusOK {
		t.Fatalf("register review = %d", code)
	}
	b, _ := os.ReadFile(log)
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "f.txt") || strings.Contains(l, "diff") || strings.Contains(l, "ls-tree") {
			t.Errorf("register ran %q", l)
		}
	}
}

// Review I4: the tree's enter on a review row reads the review's overview
// alone (no file list, counts or summaries), and its error status passes
// through: only a review or overview that is gone is a 404.
func TestReviewOverviewEndpoint(t *testing.T) {
	t.Parallel()
	ts, _, sha, id := reviewServer(t, webReviewDocWithOverview)
	var got struct {
		Label           string `json:"label"`
		OverviewMd      []any  `json:"overviewMd"`
		OverviewAnchors []any  `json:"overviewAnchors"`
		OverviewTip     string `json:"overviewTip"`
		OverviewText    string `json:"overviewText"`
		Files           []any  `json:"files"`
	}
	if code := getJSON(t, ts, "/api/review/"+id+"/overview", &got); code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if got.Label == "" || len(got.OverviewMd) == 0 || len(got.OverviewAnchors) != 3 || got.OverviewTip != sha || got.OverviewText == "" || got.Files != nil {
		t.Fatalf("got %+v", got)
	}
	if code := getJSON(t, ts, "/api/review/nope-1/overview", &got); code != http.StatusNotFound {
		t.Fatalf("gone review = %d, want 404", code)
	}
	nts, _, _, nid := reviewServer(t, webReviewDoc)
	if code := getJSON(t, nts, "/api/review/"+nid+"/overview", &got); code != http.StatusNotFound {
		t.Fatalf("review without an overview = %d, want 404", code)
	}
}
