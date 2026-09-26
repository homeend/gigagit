package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type fcBody struct {
	Lines []struct {
		Text string  `json:"text"`
		Tok  [][]any `json:"tok"`
	} `json:"lines"`
	TooLarge bool `json:"too_large"`
	Missing  bool `json:"missing"`
}

func TestFileContentWorktreeCommitAndMissing(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3) // f.txt: "content 3\n" at HEAD
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("disk 1\r\ndisk 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))
	var b fcBody
	if code := getJSON(t, ts, "/api/file-content?path=f.txt", &b); code != http.StatusOK ||
		len(b.Lines) != 2 || b.Lines[0].Text != "disk 1" || b.Lines[1].Text != "disk 2" {
		t.Fatalf("worktree: code=%d body=%+v", code, b)
	}
	b = fcBody{}
	if code := getJSON(t, ts, "/api/file-content?src=commit&rev=HEAD&path=f.txt", &b); code != http.StatusOK ||
		len(b.Lines) != 1 || b.Lines[0].Text != "content 3" {
		t.Fatalf("commit: code=%d body=%+v", code, b)
	}
	b = fcBody{}
	if code := getJSON(t, ts, "/api/file-content?path=gone.txt", &b); code != http.StatusOK || !b.Missing {
		t.Fatalf("missing: code=%d body=%+v", code, b)
	}
}

func TestFileContentRefusesUnsafeAndUnknown(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for _, q := range []string{"", "?path=-x", "?path=f.txt&src=nope", "?path=f.txt&src=commit", "?path=f.txt&src=commit&rev=-n", "?path=f.txt&src=shelf"} {
		var e map[string]any
		if code := getAny(t, ts, "/api/file-content"+q, &e); code != http.StatusBadRequest {
			t.Errorf("%q: code %d, want 400", q, code)
		}
	}
}

func TestFileContentTokensAndTooLarge(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	write(t, dir, "a.go", "package a\n\nvar x = 1\n")
	big := make([]byte, domain.MaxDiffBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))
	var b fcBody
	if code := getJSON(t, ts, "/api/file-content?path=a.go", &b); code != http.StatusOK || len(b.Lines) != 3 || len(b.Lines[0].Tok) == 0 {
		t.Fatalf("a.go: code=%d body=%+v, want 3 lines with runs on line 1", code, b)
	}
	b = fcBody{}
	if code := getJSON(t, ts, "/api/file-content?path=big.txt", &b); code != http.StatusOK || !b.TooLarge || len(b.Lines) != 0 {
		t.Fatalf("big: code=%d too_large=%v lines=%d", code, b.TooLarge, len(b.Lines))
	}
}

func TestFileContentShelfEntry(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("shelved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := domain.Open(dir)
	ts := serve(t, New(svc))
	if code := postJSON(t, ts, "/api/shelf", `{"path":"wip.txt","state":"untracked"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("shelf add: %d", code)
	}
	es, err := svc.ShelfList(t.Context(), "", 0, 0)
	if err != nil || len(es) != 1 {
		t.Fatalf("list: %v %v", es, err)
	}
	var b fcBody
	if code := getJSON(t, ts, "/api/file-content?src=shelf&rev="+es[0].ID+"&path=wip.txt", &b); code != http.StatusOK ||
		len(b.Lines) != 1 || b.Lines[0].Text != "shelved" {
		t.Fatalf("shelf: code=%d body=%+v", code, b)
	}
}
