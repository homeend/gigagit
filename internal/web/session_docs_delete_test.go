package web

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Review Focus 4: x on a document row backgrounds it — the row stays, the
// file stays; esc too. Nothing is unlinked.
func TestCloseIsLocalOnADocumentRow(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	writeDoc(t, dir, "20260101T220100-cd34.toml", noteDoc)
	s.docs.Reload(root)
	s.followDocs()
	ts := serve(t, s)
	wt := s.service().Root()
	f, _ := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"})
	for _, body := range []string{
		`{"op":"close","id":"20260101T220000-ab12","tab":"t1"}`,
		`{"op":"close","id":"20260101T220000-ab12","tab":"t1","everywhere":true}`,
		`{"op":"close","id":"` + f.ID + `","tab":"t1"}`,
		`{"op":"close","id":"` + f.ID + `","tab":"t1","everywhere":true}`,
	} {
		if code := postJSON(t, ts, "/api/open-files", body, "application/json", "", nil); code != http.StatusOK {
			t.Fatalf("%s: %d", body, code)
		}
	}
	if _, ok := s.ofs.resolve(wt, "20260101T220000-ab12", ""); !ok {
		t.Fatal("x took the overview row away")
	}
	if _, ok := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"}); !ok {
		t.Fatal("x took the noted file away")
	}
	for _, n := range []string{"20260101T220000-ab12.md", "20260101T220100-cd34.toml"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("%s was unlinked", n)
		}
	}
	if s.docs.NoteCount(root, "f.txt") != 1 {
		t.Fatal("x dropped the note")
	}
}

func TestSessionDocsDeleteOverviewUnlinksAndDelists(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(root)
	s.followDocs()
	ts := serve(t, s)
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-overview","id":"20260101T220000-ab12"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "20260101T220000-ab12.md")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}
	if _, ok := s.ofs.resolve(s.service().Root(), "20260101T220000-ab12", ""); ok {
		t.Fatal("the row is still listed")
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-overview","id":"20260101T220000-ab12"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("delete again: %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"drop","id":"x"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Fatalf("unknown op: %d, want 400", code)
	}
}

func TestSessionDocsDeleteNotesOnAFile(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220100-cd34.toml", noteDoc)
	writeDoc(t, dir, "20260101T220200-ef56.toml", noteDoc)
	s.docs.Reload(root)
	s.followDocs()
	ts := serve(t, s)
	var ans struct {
		Deleted int `json:"deleted"`
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-note","id":"20260101T220100-cd34"}`, "application/json", "", &ans); code != http.StatusOK || ans.Deleted != 1 {
		t.Fatalf("delete one: %d %+v", code, ans)
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-notes","path":"f.txt"}`, "application/json", "", &ans); code != http.StatusOK || ans.Deleted != 1 {
		t.Fatalf("delete all: %d %+v", code, ans)
	}
	if s.docs.NoteCount(root, "f.txt") != 0 {
		t.Fatal("notes left in the store")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("files left: %v", ents)
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-notes","path":"f.txt"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("delete all again: %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-notes","path":"../x"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Fatalf("bad path: %d, want 400", code)
	}
}

// A refused unlink keeps the row and the file and says so (409).
func TestSessionDocsDeleteRefusedSaysSo(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a directory mode does not refuse an unlink here")
	}
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(root)
	s.followDocs()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	ts := serve(t, s)
	code, body := postJSONRaw(t, ts, "/api/session-docs", `{"op":"delete-overview","id":"20260101T220000-ab12"}`)
	if code != http.StatusConflict || body["error"] != "could not delete overview 20260101T220000-ab12" {
		t.Fatalf("refused delete: %d %v", code, body)
	}
	if _, ok := s.ofs.resolve(s.service().Root(), "20260101T220000-ab12", ""); !ok {
		t.Fatal("a refused delete took the row away")
	}
}

// An overview row carries its document's author, creation time and anchor
// count (the TUI's row).
func TestOverviewRowsCarryAuthorCreatedAndAnchors(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(root)
	s.followDocs()
	var f struct {
		ID, Author, Created string
		Anchors             int
	}
	for _, x := range s.ofList(s.service().Root()) {
		if x.Source == "overview" {
			f.ID, f.Author, f.Created, f.Anchors = x.ID, x.Author, x.Created, x.Anchors
		}
	}
	if f.ID != "20260101T220000-ab12" || f.Author != "agent" || f.Created != "2026-01-01T22:00:00Z" || f.Anchors != 1 {
		t.Fatalf("row = %+v", f)
	}
}
