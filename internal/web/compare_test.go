package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type compareResp struct {
	A            string `json:"a"`
	B            string `json:"b"`
	AHash        string `json:"a_hash"`
	BHash        string `json:"b_hash"`
	OriginsError string `json:"origins_error"`
	Files        []struct {
		Path    string `json:"path"`
		Status  string `json:"status"`
		OldPath string `json:"old_path"`
		Origin  string `json:"origin"`
	} `json:"files"`
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// compareRepo builds main and side sharing a base commit, each with one file
// only it touched plus one file BOTH touched — so a per-side origin filter
// that simply reported "everything" would be visibly wrong.
func compareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write(t, dir, "shared.txt", "base\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")

	gitRun(t, dir, "checkout", "-b", "side")
	write(t, dir, "shared.txt", "side\n")
	write(t, dir, "side.txt", "s\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side work")

	gitRun(t, dir, "checkout", "main")
	write(t, dir, "shared.txt", "main\n")
	write(t, dir, "main.txt", "m\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "main work")
	return dir
}

func TestCompareBranches(t *testing.T) {
	dir := compareRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	var body compareResp
	if code := getJSON(t, ts, "/api/compare?a=main&b=side", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if body.OriginsError != "" {
		t.Fatalf("origins_error = %q, want none (the branches share a base)", body.OriginsError)
	}
	if body.AHash == "" || body.BHash == "" || body.AHash == body.BHash {
		t.Fatalf("tips = %q / %q", body.AHash, body.BHash)
	}
	got := map[string][2]string{} // path -> {status, origin}
	for _, f := range body.Files {
		got[f.Path] = [2]string{f.Status, f.Origin}
	}
	// left = main (a), right = side (b): main.txt exists only on the left,
	// side.txt only on the right, shared.txt differs on both.
	want := map[string][2]string{
		"main.txt":   {"D", "a"},
		"side.txt":   {"A", "b"},
		"shared.txt": {"M", "both"},
	}
	for path, w := range want {
		if got[path] != w {
			t.Errorf("%s = %v, want %v (all: %v)", path, got[path], w, got)
		}
	}
	if len(body.Files) != len(want) {
		t.Errorf("files = %v, want exactly %d", got, len(want))
	}
}

// The per-file diff of a compare row must read the two BRANCH TIPS, not a
// commit and its parent — the tips the compare response just handed back.
func TestCompareRevDiff(t *testing.T) {
	dir := compareRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	var cmp compareResp
	if code := getJSON(t, ts, "/api/compare?a=main&b=side", &cmp); code != http.StatusOK {
		t.Fatalf("compare code = %d", code)
	}
	q := url.Values{"left": {cmp.AHash}, "right": {cmp.BHash}, "path": {"shared.txt"}, "status": {"M"}}
	var d wtDiffResp
	if code := getJSON(t, ts, "/api/diff?"+q.Encode(), &d); code != http.StatusOK {
		t.Fatalf("diff code = %d", code)
	}
	if len(d.Rows) == 0 {
		t.Fatal("no diff rows")
	}
	var changed int
	for _, row := range d.Rows {
		if row.Kind == "same" {
			continue
		}
		changed++
		// left is main's version, right is side's — a swap here would still
		// produce a "changed" row, so assert the actual sides.
		if row.Left != "main" || row.Right != "side" {
			t.Errorf("row = %+v, want left=main right=side", row)
		}
	}
	if changed != 1 {
		t.Fatalf("changed rows = %d, want 1: %+v", changed, d.Rows)
	}
}

func TestCompareUnrelatedHistories(t *testing.T) {
	dir := compareRepo(t)
	// An orphan branch shares no ancestor: the file list still stands, only
	// the per-side origin filter is undefined.
	gitRun(t, dir, "checkout", "--orphan", "lonely")
	gitRun(t, dir, "rm", "-rf", "--cached", ".")
	write(t, dir, "lonely.txt", "l\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "lonely root")
	ts := serve(t, New(domain.Open(dir)))

	var body compareResp
	if code := getJSON(t, ts, "/api/compare?a=main&b=lonely", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if body.OriginsError != "no common ancestor" {
		t.Errorf("origins_error = %q", body.OriginsError)
	}
	if len(body.Files) == 0 {
		t.Error("no files — the comparison itself must survive a missing merge base")
	}
	for _, f := range body.Files {
		if f.Origin != "" {
			t.Errorf("%s carries origin %q with no merge base", f.Path, f.Origin)
		}
	}
}

func TestCompareRejects(t *testing.T) {
	dir := compareRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	cases := []struct {
		path string
		want int
	}{
		{"/api/compare?a=main", http.StatusBadRequest},
		{"/api/compare?b=side", http.StatusBadRequest},
		{"/api/compare?a=main&b=" + url.QueryEscape("--upload-pack=x"), http.StatusBadRequest},
		{"/api/compare?a=main&b=nope", http.StatusNotFound},
		{"/api/compare?a=nope&b=side", http.StatusNotFound},
		// a tag or a raw sha is not a local branch: the allowlist is by name
		{"/api/compare?a=main&b=HEAD", http.StatusNotFound},
		{"/api/diff?left=" + url.QueryEscape("-x") + "&right=main&path=f.txt", http.StatusBadRequest},
		{"/api/diff?left=main&path=f.txt", http.StatusBadRequest}, // right missing
	}
	for _, c := range cases {
		if code := getJSON(t, ts, c.path, nil); code != c.want {
			t.Errorf("GET %s = %d, want %d", c.path, code, c.want)
		}
	}
}
