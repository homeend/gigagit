package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// addShelfFile shelves a working-tree file and returns the entry id.
func addShelfFile(t *testing.T, ts *httptest.Server, path string) string {
	t.Helper()
	var out struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	if code := postJSON(t, ts, "/api/shelf", `{"path":"`+path+`","state":"unstaged"}`, "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("shelf add: code = %d", code)
	}
	return out.Entry.ID
}

// A frozen copy is only useful if it can be put back: restore writes an
// entry's bytes to a path, even after the source file has changed underneath.
func TestRestoreShelfFileEntry(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("the good version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pair := serve(t, New(domain.Open(dir)))
	id := addShelfFile(t, pair, "f.txt")

	// Wreck the working copy; the shelf still holds the good bytes.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("ruined\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"op":"restore-entry","store":"shelf","id":"` + id + `","dest":"restored.txt"}`
	events := readSSE(t, pair, startOpBody(t, pair, body), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	got, err := os.ReadFile(filepath.Join(dir, "restored.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "the good version\n" {
		t.Errorf("restored = %q, want the shelved bytes", got)
	}
}

// One file out of a SHELVED COMMIT: the entry holds a whole archive, and a
// single path from it can be written back.
func TestRestoreFileFromShelvedCommit(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "kept.txt"), []byte("frozen\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "kept.txt")
	gitRun(t, dir, "commit", "-m", "add kept.txt")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	pair := serve(t, New(domain.Open(dir)))

	var out struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	if code := postJSON(t, pair, "/api/shelf", `{"sha":"`+sha+`","label":"spike"}`, "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("shelf add: code = %d", code)
	}

	body := `{"op":"restore-entry","store":"shelf","id":"` + out.Entry.ID + `","path":"kept.txt","dest":"out.txt"}`
	events := readSSE(t, pair, startOpBody(t, pair, body), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	got, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "frozen\n" {
		t.Errorf("restored = %q, want the frozen bytes", got)
	}
}

// A shelved commit re-applies onto the current branch. The commit object
// still exists here, so this is the live cherry-pick lane.
func TestShelfCherryPickLiveLane(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("from side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "side.txt")
	gitRun(t, dir, "commit", "-m", "side work")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "main")
	pair := serve(t, New(domain.Open(dir)))

	var out struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	postJSON(t, pair, "/api/shelf", `{"sha":"`+sha+`","label":"side"}`, "application/json", "", &out)

	body := `{"op":"shelf-cherry-pick","id":"` + out.Entry.ID + `"}`
	events := readSSE(t, pair, startOpBody(t, pair, body), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if subj := gitRun(t, dir, "log", "-1", "--format=%s", "main"); subj != "side work" {
		t.Errorf("main tip = %q, want the shelved commit applied", subj)
	}
}

// Guards: a file entry is not cherry-pickable, and an unknown id or store is
// refused before anything runs.
func TestEntryOpGuards(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pair := serve(t, New(domain.Open(dir)))
	id := addShelfFile(t, pair, "f.txt")

	cases := []struct {
		name, body string
		want       int
	}{
		{"file entry cherry-pick", `{"op":"shelf-cherry-pick","id":"` + id + `"}`, http.StatusUnprocessableEntity},
		{"unknown entry", `{"op":"shelf-cherry-pick","id":"nope"}`, http.StatusNotFound},
		{"no id", `{"op":"restore-entry","store":"shelf","dest":"x.txt"}`, http.StatusBadRequest},
		{"no dest", `{"op":"restore-entry","store":"shelf","id":"` + id + `"}`, http.StatusBadRequest},
		{"unknown store", `{"op":"restore-entry","store":"attic","id":"` + id + `","dest":"x.txt"}`, http.StatusBadRequest},
		{"absolute dest", `{"op":"restore-entry","store":"shelf","id":"` + id + `","dest":"/tmp/x.txt"}`, http.StatusBadRequest},
		{"escaping dest", `{"op":"restore-entry","store":"shelf","id":"` + id + `","dest":"../x.txt"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		if code := postJSON(t, pair, "/api/op", c.body, "application/json", "", nil); code != c.want {
			t.Errorf("%s: code = %d, want %d", c.name, code, c.want)
		}
	}
}

// A bookmark points at live content, so restoring one writes what it points
// at TODAY — that is the difference from a shelf entry.
func TestRestoreBookmark(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "live.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pair := serve(t, New(domain.Open(dir)))

	var out struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	if code := postJSON(t, pair, "/api/bookmarks", `{"path":"live.txt","state":"untracked"}`, "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("bookmark add: code = %d", code)
	}
	if err := os.WriteFile(filepath.Join(dir, "live.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	body := `{"op":"restore-entry","store":"bookmarks","id":"` + out.Entry.ID + `","dest":"copy.txt"}`
	events := readSSE(t, pair, startOpBody(t, pair, body), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	got, err := os.ReadFile(filepath.Join(dir, "copy.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "v2" {
		t.Errorf("restored = %q, want the CURRENT content v2 (a bookmark is live)", got)
	}
}
