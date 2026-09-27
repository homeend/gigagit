package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

type stampBody struct {
	Stamp string `json:"stamp"`
}

// F's preview follows the disk by re-stating its file: the stamp names a
// size+mtime, "missing" for a file that is not there, and changes when the
// file does.
func TestFileStampFollowsTheDisk(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	var a stampBody
	if code := getJSON(t, ts, "/api/file-stamp?path=f.txt", &a); code != http.StatusOK || a.Stamp == "" || a.Stamp == "missing" {
		t.Fatalf("present: code=%d stamp=%q", code, a.Stamp)
	}
	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("longer content now\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(f, later, later); err != nil {
		t.Fatal(err)
	}
	var b stampBody
	if code := getJSON(t, ts, "/api/file-stamp?path=f.txt", &b); code != http.StatusOK || b.Stamp == a.Stamp || b.Stamp == "" {
		t.Fatalf("changed: code=%d %q → %q", code, a.Stamp, b.Stamp)
	}
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	var c stampBody
	if code := getJSON(t, ts, "/api/file-stamp?path=f.txt", &c); code != http.StatusOK || c.Stamp != "missing" {
		t.Fatalf("removed: code=%d stamp=%q", code, c.Stamp)
	}
}

func TestFileStampRefusesUnsafeAndEscapingPaths(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for _, q := range []string{"", "?path=-x", "?path=../gg-no-such-file-xyz", "?path=../../etc/hostname", "?path=/etc/hostname"} {
		var e map[string]any
		if code := getAny(t, ts, "/api/file-stamp"+q, &e); code != http.StatusBadRequest {
			t.Errorf("%q: code %d, want 400", q, code)
		}
	}
}

// The content carries the stamp it was read at, so the preview's first
// check has a baseline no newer than the lines it shows.
func TestFileContentCarriesTheWorktreeStamp(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	var fc struct {
		Stamp string `json:"stamp"`
	}
	var st stampBody
	getJSON(t, ts, "/api/file-content?src=worktree&path=f.txt", &fc)
	getJSON(t, ts, "/api/file-stamp?path=f.txt", &st)
	if fc.Stamp == "" || fc.Stamp != st.Stamp {
		t.Fatalf("content stamp %q, file-stamp %q", fc.Stamp, st.Stamp)
	}
	fc.Stamp = ""
	getJSON(t, ts, "/api/file-content?src=worktree&path=gone.txt", &fc)
	if fc.Stamp != "missing" {
		t.Fatalf("missing file's content stamp %q, want missing", fc.Stamp)
	}
}
