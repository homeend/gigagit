package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type wtFilesResp struct {
	Files []struct {
		Path      string `json:"path"`
		Untracked bool   `json:"untracked"`
	} `json:"files"`
	Total   int  `json:"total"`
	Next    int  `json:"next"`
	Limited bool `json:"limited"`
	Gen     int  `json:"gen"`
}

func (r wtFilesResp) rows() string {
	var b []string
	for _, f := range r.Files {
		s := f.Path
		if f.Untracked {
			s += "(u)"
		}
		b = append(b, s)
	}
	return strings.Join(b, " ")
}

func writeFiles(t *testing.T, dir string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The list is the working tree on disk: tracked minus deleted (either side),
// plus untracked (marked), plus a staged new file (it is in the index).
func TestWorktreeFilesListsDiskFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	writeFiles(t, dir, "a.go", "b.go", "dir/c.go", "gone.go")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c1")
	if err := os.Remove(filepath.Join(dir, "b.go")); err != nil { // unstaged D
		t.Fatal(err)
	}
	gitRun(t, dir, "rm", "-q", "gone.go") // staged D
	writeFiles(t, dir, "n.txt", "s.go")
	gitRun(t, dir, "add", "s.go") // a staged new file
	ts := serve(t, New(domain.Open(dir)))

	var got wtFilesResp
	if code := getJSON(t, ts, "/api/worktree-files?fresh=1", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if want := "a.go dir/c.go n.txt(u) s.go"; got.rows() != want {
		t.Fatalf("rows = %q, want %q", got.rows(), want)
	}
	if got.Total != 4 || got.Next != 0 || got.Limited || got.Gen == 0 {
		t.Fatalf("total/next/limited/gen = %d/%d/%v/%d, want 4/0/false/>0", got.Total, got.Next, got.Limited, got.Gen)
	}
}

// A query ranks fuzzily (a subsequence, not a substring) over the whole list;
// no match is an empty list, never null.
func TestWorktreeFilesFuzzy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	writeFiles(t, dir, "cmd/gg/main.go", "internal/web/search.go", "README.md")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c1")
	ts := serve(t, New(domain.Open(dir)))

	var got wtFilesResp
	getJSON(t, ts, "/api/worktree-files?fresh=1&q=cgmain", &got)
	if got.rows() != "cmd/gg/main.go" || got.Total != 3 {
		t.Fatalf("rows = %q total %d, want cmd/gg/main.go of 3", got.rows(), got.Total)
	}
	var none wtFilesResp
	getJSON(t, ts, "/api/worktree-files?q=zzzznope", &none)
	if none.Files == nil || len(none.Files) != 0 {
		t.Fatalf("files = %#v, want an empty list", none.Files)
	}
}

// No query pages through the sorted list 200 at a time; a query caps at 200.
func TestWorktreeFilesPages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	var paths []string
	for i := 0; i < 450; i++ {
		paths = append(paths, fmt.Sprintf("f%03d.txt", i))
	}
	writeFiles(t, dir, paths...)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c1")
	ts := serve(t, New(domain.Open(dir)))

	var p0, p1, p2, far, ranked wtFilesResp
	getJSON(t, ts, "/api/worktree-files?fresh=1", &p0)
	getJSON(t, ts, "/api/worktree-files?offset=200", &p1)
	getJSON(t, ts, "/api/worktree-files?offset=400", &p2)
	getJSON(t, ts, "/api/worktree-files?offset=9999", &far)
	getJSON(t, ts, "/api/worktree-files?q=f&offset=400", &ranked)
	if len(p0.Files) != 200 || p0.Next != 200 || p0.Files[0].Path != "f000.txt" {
		t.Fatalf("page 0 = %d rows next %d first %v", len(p0.Files), p0.Next, p0.Files[0])
	}
	if len(p1.Files) != 200 || p1.Next != 400 || p1.Files[0].Path != "f200.txt" {
		t.Fatalf("page 1 = %d rows next %d", len(p1.Files), p1.Next)
	}
	if len(p2.Files) != 50 || p2.Next != 0 || p2.Files[49].Path != "f449.txt" {
		t.Fatalf("page 2 = %d rows next %d", len(p2.Files), p2.Next)
	}
	if len(far.Files) != 0 || far.Next != 0 {
		t.Fatalf("past the end = %d rows next %d, want none", len(far.Files), far.Next)
	}
	if p0.Gen != p1.Gen || p1.Gen != p2.Gen {
		t.Fatalf("gens %d/%d/%d: pages of one read must share it", p0.Gen, p1.Gen, p2.Gen)
	}
	if len(ranked.Files) != 200 || !ranked.Limited || ranked.Next != 0 {
		t.Fatalf("ranked = %d rows limited %v next %d, want 200/true/0 (offset ignored)", len(ranked.Files), ranked.Limited, ranked.Next)
	}
	for _, bad := range []string{"abc", "-1"} {
		if code := getJSON(t, ts, "/api/worktree-files?offset="+bad, nil); code != http.StatusBadRequest {
			t.Errorf("offset=%s: status %d, want 400", bad, code)
		}
	}
}

// The list is read once per F open (fresh=1); without it the cache answers,
// so a file created since is not listed until the next fresh read.
func TestWorktreeFilesCacheAndFresh(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	var first, cached, fresh wtFilesResp
	getJSON(t, ts, "/api/worktree-files?fresh=1", &first)
	writeFiles(t, dir, "new.txt")
	getJSON(t, ts, "/api/worktree-files", &cached)
	getJSON(t, ts, "/api/worktree-files?fresh=1", &fresh)
	if cached.Gen != first.Gen || cached.rows() != "f.txt" {
		t.Fatalf("cached = %q gen %d, want the first read (gen %d)", cached.rows(), cached.Gen, first.Gen)
	}
	if fresh.Gen == first.Gen || fresh.rows() != "f.txt new.txt(u)" {
		t.Fatalf("fresh = %q gen %d, want a re-read listing new.txt", fresh.rows(), fresh.Gen)
	}
}
