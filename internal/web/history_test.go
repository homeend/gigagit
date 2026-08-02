package web

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestResolveEndpoint(t *testing.T) {
	dir := newRepoDir(t, 3)
	head := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	var r struct{ Hash, Subject string }
	if code := getJSON(t, ts, "/api/resolve?rev=HEAD", &r); code != http.StatusOK {
		t.Fatalf("resolve HEAD: code %d", code)
	}
	if r.Hash != head {
		t.Fatalf("hash = %q, want %q", r.Hash, head)
	}
	if len(r.Hash) != 40 {
		t.Fatalf("want FULL sha, got %d chars", len(r.Hash))
	}
	if r.Subject != "c3" {
		t.Fatalf("subject = %q, want c3", r.Subject)
	}

	if code := getJSON(t, ts, "/api/resolve?rev=nope-nothing", nil); code != http.StatusNotFound {
		t.Fatalf("unknown rev: code %d, want 404", code)
	}
	if code := getJSON(t, ts, "/api/resolve?rev=--all", nil); code != http.StatusBadRequest {
		t.Fatalf("option-shaped rev: code %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/resolve", nil); code != http.StatusBadRequest {
		t.Fatalf("empty rev: code %d, want 400", code)
	}
}

type fileLogTestRow struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Status  string `json:"status"`
	Path    string `json:"path"`
	OldPath string `json:"old_path"`
}

func TestFileLogEndpoint(t *testing.T) {
	dir := newRepoDir(t, 3) // c1..c3 each rewriting f.txt
	gitRun(t, dir, "mv", "f.txt", "g.txt")
	gitRun(t, dir, "commit", "-m", "rename it")
	ts := serve(t, New(domain.Open(dir)))

	var r struct{ Rows []fileLogTestRow }
	if code := getJSON(t, ts, "/api/filelog?path=g.txt", &r); code != http.StatusOK {
		t.Fatalf("filelog: code %d", code)
	}
	// newest first: rename, c3, c2, c1 (--follow crosses the rename)
	if len(r.Rows) != 4 {
		t.Fatalf("rows = %d, want 4 (follow must cross the rename): %+v", len(r.Rows), r.Rows)
	}
	if r.Rows[0].Status != "R" || r.Rows[0].OldPath != "f.txt" || r.Rows[0].Path != "g.txt" {
		t.Fatalf("rename row = %+v", r.Rows[0])
	}
	if r.Rows[0].Subject != "rename it" || r.Rows[3].Status != "A" {
		t.Fatalf("order/status wrong: first %+v last %+v", r.Rows[0], r.Rows[3])
	}
	if len(r.Rows[0].Hash) != 40 || len(r.Rows[0].Short) != 8 || r.Rows[0].Time == 0 {
		t.Fatalf("row fields: %+v", r.Rows[0])
	}

	// no history is an EMPTY list, not an error (the TUI "(no history)" rule)
	r.Rows = nil
	if code := getJSON(t, ts, "/api/filelog?path=never-existed.txt", &r); code != http.StatusOK {
		t.Fatalf("no-history path: code %d", code)
	}
	if len(r.Rows) != 0 {
		t.Fatalf("no-history rows = %+v, want empty", r.Rows)
	}

	if code := getJSON(t, ts, "/api/filelog?path=--foo", nil); code != http.StatusBadRequest {
		t.Fatalf("option-shaped path: code %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/filelog?path=g.txt&rev=--all", nil); code != http.StatusBadRequest {
		t.Fatalf("option-shaped rev: code %d, want 400", code)
	}
}

type blameTestRow struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Summary string `json:"summary"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
}

func TestBlameEndpoint(t *testing.T) {
	dir := newRepoDir(t, 3)
	// one uncommitted appended line on top of committed content
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("content 3\nuncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	// working-tree blame: rev omitted
	var r struct{ Lines []blameTestRow }
	if code := getJSON(t, ts, "/api/blame?path=f.txt", &r); code != http.StatusOK {
		t.Fatalf("blame: code %d", code)
	}
	if len(r.Lines) != 2 {
		t.Fatalf("lines = %d, want 2: %+v", len(r.Lines), r.Lines)
	}
	if r.Lines[0].Hash == "" || r.Lines[0].Summary != "c3" || r.Lines[0].Text != "content 3" {
		t.Fatalf("committed line = %+v", r.Lines[0])
	}
	if r.Lines[1].Hash != "" || r.Lines[1].Short != "" || r.Lines[1].Text != "uncommitted" {
		t.Fatalf("uncommitted line must have empty hash: %+v", r.Lines[1])
	}

	// blame AT a commit ignores the working tree; a second identical call is
	// served from the blame LRU and must return the same rows (equality, not
	// timing — the cache is an implementation detail)
	var atHead, again struct{ Lines []blameTestRow }
	if code := getJSON(t, ts, "/api/blame?path=f.txt&rev=HEAD", &atHead); code != http.StatusOK {
		t.Fatalf("blame@HEAD: code %d", code)
	}
	if len(atHead.Lines) != 1 || atHead.Lines[0].Text != "content 3" {
		t.Fatalf("blame@HEAD lines = %+v", atHead.Lines)
	}
	if code := getJSON(t, ts, "/api/blame?path=f.txt&rev=HEAD", &again); code != http.StatusOK {
		t.Fatalf("blame@HEAD (repeat): code %d", code)
	}
	if !reflect.DeepEqual(atHead, again) {
		t.Fatalf("repeat blame differs: %+v vs %+v", atHead, again)
	}

	// untracked path: git blame fails -> 500, overlay never opens
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := getJSON(t, ts, "/api/blame?path=untracked.txt", nil); code != http.StatusInternalServerError {
		t.Fatalf("untracked blame: code %d, want 500", code)
	}
	if code := getJSON(t, ts, "/api/blame?path=--foo", nil); code != http.StatusBadRequest {
		t.Fatalf("option-shaped path: code %d, want 400", code)
	}
}
