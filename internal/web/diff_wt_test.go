package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// wtDiffResp decodes the /api/diff JSON needed by these tests.
type wtDiffResp struct {
	Rows []struct {
		Kind  string `json:"kind"`
		Left  string `json:"left"`
		Right string `json:"right"`
	} `json:"rows"`
	Binary bool `json:"binary"`
}

func rightContains(d wtDiffResp, s string) bool {
	for _, r := range d.Rows {
		if strings.Contains(r.Right, s) {
			return true
		}
	}
	return false
}

func TestWorktreeDiffUnstagedStagedAndCacheBypass(t *testing.T) {
	dir := newRepoDir(t, 1) // f.txt committed as "content 1\n"
	ts := serve(t, New(domain.Open(dir)))
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// unstaged edit: index("content 1") vs worktree("content 2")
	write("content 2\n")
	var d wtDiffResp
	if code := getJSON(t, ts, "/api/diff?wt=unstaged&path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("unstaged code = %d", code)
	}
	if !rightContains(d, "content 2") {
		t.Fatalf("unstaged diff misses new content: %+v", d.Rows)
	}

	// stage it: staged form shows HEAD("content 1") vs index("content 2"),
	// unstaged form goes quiet (index == worktree).
	gitRun(t, dir, "add", "f.txt")
	if code := getJSON(t, ts, "/api/diff?wt=staged&path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("staged code = %d", code)
	}
	if !rightContains(d, "content 2") {
		t.Fatalf("staged diff misses index content: %+v", d.Rows)
	}
	if code := getJSON(t, ts, "/api/diff?wt=unstaged&path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("unstaged-clean code = %d", code)
	}
	for _, r := range d.Rows {
		if r.Kind != "same" {
			t.Fatalf("unstaged diff not clean after staging: %+v", d.Rows)
		}
	}

	// cache-bypass requirement (spec §B): a second request after the file
	// changed on disk must reflect the NEW content.
	write("content 3\n")
	if code := getJSON(t, ts, "/api/diff?wt=unstaged&path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("rebypass code = %d", code)
	}
	if !rightContains(d, "content 3") {
		t.Fatalf("stale working-tree diff (cache not bypassed): %+v", d.Rows)
	}
}

func TestWorktreeDiffUntrackedAndDeleted(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	// untracked: empty old side, all-add rows
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var d wtDiffResp
	if code := getJSON(t, ts, "/api/diff?wt=unstaged&path=new.txt", &d); code != http.StatusOK {
		t.Fatalf("untracked code = %d", code)
	}
	if len(d.Rows) == 0 {
		t.Fatal("untracked diff has no rows") // vacuous-pass guard
	}
	for _, r := range d.Rows {
		if r.Kind != "add" {
			t.Fatalf("untracked diff row not add: %+v", d.Rows)
		}
	}

	// deleted in worktree: empty new side, all-del rows
	if err := os.Remove(filepath.Join(dir, "f.txt")); err != nil {
		t.Fatal(err)
	}
	if code := getJSON(t, ts, "/api/diff?wt=unstaged&path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("deleted code = %d", code)
	}
	if len(d.Rows) == 0 {
		t.Fatal("deleted diff has no rows") // vacuous-pass guard
	}
	for _, r := range d.Rows {
		if r.Kind != "del" {
			t.Fatalf("deleted diff row not del: %+v", d.Rows)
		}
	}
}

func TestWorktreeDiffBadRequests(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	for _, q := range []string{
		"wt=bogus&path=f.txt",             // unknown wt mode
		"wt=unstaged",                     // missing path
		"wt=unstaged&path=--evil",         // argv-unsafe path
		"wt=unstaged&path=f.txt&sha=HEAD", // wt and sha are mutually exclusive
	} {
		if code := getJSON(t, ts, "/api/diff?"+q, nil); code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", q, code)
		}
	}
}
