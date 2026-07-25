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

// hunkTagResp decodes the inline-hunk additions to /api/diff.
type hunkTagResp struct {
	Rows []struct {
		Kind string `json:"kind"`
		Hunk *int   `json:"hunk"`
	} `json:"rows"`
	Hunks *struct {
		Count int    `json:"count"`
		Hash  string `json:"hash"`
	} `json:"hunks"`
}

func TestWorktreeDiffInlineHunkTags(t *testing.T) {
	dir := newRepoDir(t, 1)
	big := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\n"
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "big.txt")
	gitRun(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "big")
	edited := strings.Replace(strings.Replace(big, "l2\n", "l2 EDITED\n", 1), "l10\n", "l10 EDITED\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	var d hunkTagResp
	if code := getJSON(t, ts, "/api/diff?wt=unstaged&path=big.txt", &d); code != http.StatusOK {
		t.Fatalf("unstaged code = %d", code)
	}
	if d.Hunks == nil || d.Hunks.Count != 2 || d.Hunks.Hash == "" {
		t.Fatalf("hunks meta = %+v, want count 2 with a hash", d.Hunks)
	}
	sawSame, want := false, 0
	for _, r := range d.Rows {
		if r.Kind == "same" {
			sawSame = true
			if r.Hunk != nil {
				t.Fatalf("context row carries a hunk tag: %+v", r)
			}
			continue
		}
		if r.Hunk == nil {
			t.Fatalf("change row missing its hunk tag: %+v", d.Rows)
		}
		if *r.Hunk != want && *r.Hunk != want+1 {
			t.Fatalf("hunk ordinals out of order: got %d after %d", *r.Hunk, want)
		}
		want = *r.Hunk
	}
	if !sawSame {
		t.Fatal("no context rows — the inline view lost the context it exists for")
	}
	if want != 1 {
		t.Fatalf("last hunk ordinal = %d, want 1", want)
	}

	// the diff's hash IS the staging freshness token
	var h struct {
		Count int    `json:"count"`
		Hash  string `json:"hash"`
	}
	if code := getJSON(t, ts, "/api/hunks?path=big.txt", &h); code != http.StatusOK {
		t.Fatal("hunks endpoint")
	}
	if h.Hash != d.Hunks.Hash || h.Count != d.Hunks.Count {
		t.Fatalf("diff meta (%d,%s) != hunks endpoint (%d,%s)", d.Hunks.Count, d.Hunks.Hash, h.Count, h.Hash)
	}
	body := `{"path":"big.txt","picks":[0],"hash":"` + d.Hunks.Hash + `"}`
	if code := postJSON(t, ts, "/api/stage-hunks", body, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("stage via diff hash code = %d", code)
	}

	// staged form and ineligible files carry no hunk metadata (fresh decode
	// targets — json.Unmarshal leaves an existing pointer when the key is
	// absent, so reusing d would false-fail)
	var staged hunkTagResp
	if code := getJSON(t, ts, "/api/diff?wt=staged&path=big.txt", &staged); code != http.StatusOK {
		t.Fatal("staged form")
	}
	if staged.Hunks != nil {
		t.Fatalf("staged diff unexpectedly tagged: %+v", staged.Hunks)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var untracked hunkTagResp
	if code := getJSON(t, ts, "/api/diff?wt=unstaged&path=new.txt", &untracked); code != http.StatusOK {
		t.Fatal("untracked form")
	}
	if untracked.Hunks != nil {
		t.Fatalf("untracked diff unexpectedly tagged: %+v", untracked.Hunks)
	}
}
