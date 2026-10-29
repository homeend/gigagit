package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
