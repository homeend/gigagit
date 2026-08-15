package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// Both stores live in the machine's state dir; every test here gets its own so
// nothing touches the developer's real bookmarks or shelf.
func isolateState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// deleteJSON issues a DELETE (the remove lane) and returns the status code.
func deleteJSON(t *testing.T, ts *httptest.Server, path string) int {
	t.Helper()
	req, err := http.NewRequest("DELETE", ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

type bmList struct {
	Entries []struct {
		ID       string `json:"id"`
		Display  string `json:"display"`
		Label    string `json:"label"`
		State    string `json:"state"`
		Path     string `json:"path"`
		Commit   string `json:"commit"`
		IsCommit bool   `json:"is_commit"`
	} `json:"entries"`
}

type shList struct {
	Entries []struct {
		ID      string `json:"id"`
		Kind    string `json:"kind"`
		Label   string `json:"label"`
		Path    string `json:"path"`
		Display string `json:"display"`
	} `json:"entries"`
	Buckets []string `json:"buckets"`
}

// A commit bookmark is path-less and keeps the label the client typed.
func TestBookmarkCommitRoundTrip(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 2)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/bookmarks", `{"sha":"`+sha+`","label":"the fix"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("add: code = %d", code)
	}
	var got bmList
	if code := getJSON(t, ts, "/api/bookmarks", &got); code != http.StatusOK {
		t.Fatalf("list: code = %d", code)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(got.Entries))
	}
	e := got.Entries[0]
	if !e.IsCommit || e.Label != "the fix" || e.Commit != sha || e.Path != "" {
		t.Errorf("entry = %+v, want a path-less commit bookmark labelled 'the fix'", e)
	}

	if code := deleteJSON(t, ts, "/api/bookmarks?id="+e.ID); code != http.StatusOK {
		t.Fatalf("remove: code = %d", code)
	}
	got = bmList{}
	getJSON(t, ts, "/api/bookmarks", &got)
	if len(got.Entries) != 0 {
		t.Errorf("entries after remove = %d, want 0", len(got.Entries))
	}
}

// A FILE bookmark takes its worktree and branch from the SERVER, never the
// wire — one POST must not be able to file an entry against another checkout.
func TestBookmarkFileUsesServedRepo(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	body := `{"path":"note.txt","state":"untracked","worktree":"/somewhere/else","branch":"evil"}`
	if code := postJSON(t, ts, "/api/bookmarks", body, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("add: code = %d", code)
	}
	var got bmList
	getJSON(t, ts, "/api/bookmarks", &got)
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(got.Entries))
	}
	e := got.Entries[0]
	if e.Path != "note.txt" || e.State != "untracked" {
		t.Errorf("entry = %+v, want note.txt as untracked", e)
	}
	if strings.Contains(e.Display, "else") {
		t.Errorf("display = %q — the wire's worktree was believed", e.Display)
	}
}

// An unknown state is refused rather than silently filed as unstaged.
func TestBookmarkRejectsUnknownState(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/bookmarks", `{"path":"f.txt","state":"whatever"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", code)
	}
	if code := postJSON(t, ts, "/api/bookmarks", `{"sha":"HEAD"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("a rev expression: code = %d, want 400 (hex only)", code)
	}
	if code := deleteJSON(t, ts, "/api/bookmarks"); code != http.StatusBadRequest {
		t.Errorf("remove with no id: code = %d, want 400", code)
	}
}

// Shelving a commit freezes its changed files; the entry lists them back.
func TestShelfCommitRoundTrip(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 2)
	if err := os.WriteFile(filepath.Join(dir, "frozen.txt"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "frozen.txt")
	gitRun(t, dir, "commit", "-m", "add frozen.txt")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/shelf", `{"sha":"`+sha+`","label":"spike"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("add: code = %d", code)
	}
	var got shList
	if code := getJSON(t, ts, "/api/shelf", &got); code != http.StatusOK {
		t.Fatalf("list: code = %d", code)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(got.Entries))
	}
	e := got.Entries[0]
	if e.Kind != "commit" || e.Label != "spike" {
		t.Errorf("entry = %+v, want a commit entry labelled 'spike'", e)
	}

	var files struct {
		Files []string `json:"files"`
	}
	if code := getJSON(t, ts, "/api/shelf/files?id="+e.ID, &files); code != http.StatusOK {
		t.Fatalf("files: code = %d", code)
	}
	if len(files.Files) != 1 || files.Files[0] != "frozen.txt" {
		t.Errorf("files = %v, want [frozen.txt]", files.Files)
	}

	if code := deleteJSON(t, ts, "/api/shelf?id="+e.ID); code != http.StatusOK {
		t.Fatalf("remove: code = %d", code)
	}
	got = shList{}
	getJSON(t, ts, "/api/shelf", &got)
	if len(got.Entries) != 0 {
		t.Errorf("entries after remove = %d, want 0", len(got.Entries))
	}
}

// A working-tree FILE can be shelved too — that is the frozen-copy case.
func TestShelfFileEntry(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/shelf", `{"path":"f.txt","state":"unstaged"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("add: code = %d", code)
	}
	var got shList
	getJSON(t, ts, "/api/shelf", &got)
	if len(got.Entries) != 1 || got.Entries[0].Kind != "file" || got.Entries[0].Path != "f.txt" {
		t.Fatalf("entries = %+v, want one file entry for f.txt", got.Entries)
	}
}
