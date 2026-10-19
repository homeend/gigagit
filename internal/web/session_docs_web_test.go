package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// attachedSrv is noteSrv with the served worktree attached to a temp docs
// directory: what a standalone gg web has after Start, what a hosted page
// has from the TUI.
func attachedSrv(t *testing.T) (s *Server, root, dir string) {
	t.Helper()
	s, root = noteSrv(t)
	dir = filepath.Join(t.TempDir(), "docs")
	if err := s.docs.Attach(root, s.service().Root(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.docs.Close)
	return s, root, dir
}

// writeDoc writes a document by hand — no gg verb — the way an agent does.
func writeDoc(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	tourDoc = "+++\nformat = 1\ntitle = \"Tour of f\"\nauthor = \"agent\"\ncreated = 2026-01-01T22:00:00Z\n+++\n# Tour\n\nSee [one](f.txt:1).\n"
	noteDoc = "format = 1\npath = \"f.txt\"\nstart = 1\nend = 1\nsummary = \"why one?\"\nauthor = \"agent\"\nlines = [\"content 1\"]\n"
)

// Review Focus 1: a document id (its file name) passes every gate a memory
// id passed; an id that is not a document id is still refused.
func TestDocumentIDsPassTheWebGates(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	writeDoc(t, dir, "20260101T220100-cd34.toml", noteDoc)
	s.docs.Reload(root)
	s.followDocs()
	ts := serve(t, s)
	var ov struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Anchors []any  `json:"anchors"`
	}
	if code := getJSON(t, ts, "/api/overview?id=20260101T220000-ab12", &ov); code != http.StatusOK || ov.Title != "Tour of f" || len(ov.Anchors) != 1 {
		t.Fatalf("overview: %d %+v", code, ov)
	}
	if code := getJSON(t, ts, "/api/overview?id=../etc", &ov); code != http.StatusBadRequest {
		t.Fatalf("a path is not an id: %d", code)
	}
	if code := getJSON(t, ts, "/api/overview?id=a.b", &ov); code != http.StatusBadRequest {
		t.Fatalf("a dot is not an id: %d", code)
	}
	res, err := http.Get(ts.URL + "/api/overview-export?id=20260101T220000-ab12")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("export: %d", res.StatusCode)
	}
	_, rep := steerAsk(t, s, `{"id":"1","cmd":"file_focus","file_id":"20260101T220000-ab12"}`)
	if !rep.OK || !strings.HasPrefix(rep.Detail, `focused overview 20260101T220000-ab12 "Tour of f"`) {
		t.Fatalf("file_focus: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"2","cmd":"overview_show","file_id":"20260101T220000-ab12"}`); !rep.OK || rep.Overviews[0].Title != "Tour of f" {
		t.Fatalf("overview_show: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"3","cmd":"note_show","note_id":"20260101T220100-cd34"}`); !rep.OK || len(rep.Notes) != 1 || rep.Notes[0].Summary != "why one?" {
		t.Fatalf("note_show: %+v", rep)
	}
}

// The follow pass lists a document written by hand, keyed by its id and
// shown under the TUI's display name; a hand-removed one leaves the list.
func TestAHandWrittenOverviewIsListedAndAHandRemovedOneLeaves(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(root)
	s.followDocs()
	wt := s.service().Root()
	if _, ok := s.ofs.resolve(wt, "20260101T220000-ab12", ""); !ok {
		t.Fatal("the document is not in the page's list")
	}
	if k, _, _ := s.ofs.pinnedEntry(wt, "20260101T220000-ab12"); k.Path != "overview-20260101T220000-ab12.md" {
		t.Fatalf("display name = %q", k.Path)
	}
	if err := os.Remove(filepath.Join(dir, "20260101T220000-ab12.md")); err != nil {
		t.Fatal(err)
	}
	s.docs.Reload(root)
	s.followDocs()
	if _, ok := s.ofs.resolve(wt, "20260101T220000-ab12", ""); ok {
		t.Fatal("a removed document stays listed")
	}
}

// overview_rm on the web deletes the document (the writer takes it back)
// and says so in the TUI's words.
func TestSteerOverviewRmDeletesTheDocument(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(root)
	s.followDocs()
	_, rep := steerAsk(t, s, `{"id":"1","cmd":"overview_rm","file_id":"20260101T220000-ab12"}`)
	if !rep.OK || rep.Detail != "removed 20260101T220000-ab12" {
		t.Fatalf("rm: %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(dir, "20260101T220000-ab12.md")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}
	if _, ok := s.ofs.resolve(s.service().Root(), "20260101T220000-ab12", ""); ok {
		t.Fatal("rm left the entry listed")
	}
}
