package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// A file shown as LINE ROWS (/api/file-content, /api/blame) takes the same
// "text now, colours later" flags as /api/diff: defer_tok=1 answers the rows
// at once with tok_pending + the content's sum, tok=1 answers the runs by
// source line for that sum.

type lineTokWire struct {
	Sum string    `json:"sum"`
	Tok [][][]any `json:"tok"`
}

type lineRowsWire struct {
	TokPending bool             `json:"tok_pending"`
	Sum        string           `json:"sum"`
	Lines      []map[string]any `json:"lines"`
}

// bigGoRepo commits a big Go file and returns the server and its sha.
func bigGoRepo(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write(t, dir, "big.go", bigGo(""))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "one")
	return serve(t, New(domain.Open(dir))), gitRun(t, dir, "rev-parse", "HEAD")
}

// checkLineTwoHalves asks base deferred, then for its colours, then (on a
// FRESH server, the store being shared) one-shot, and checks they agree.
func checkLineTwoHalves(t *testing.T, base string) {
	t.Helper()
	ts, sha := bigGoRepo(t)
	url := strings.ReplaceAll(base, "SHA", sha)
	var d lineRowsWire
	if code := getJSON(t, ts, url+"&defer_tok=1", &d); code != 200 {
		t.Fatalf("deferred: code %d", code)
	}
	if !d.TokPending || d.Sum == "" || len(d.Lines) == 0 {
		t.Fatalf("deferred: pending=%v sum=%q rows=%d; want pending rows with the sum", d.TokPending, d.Sum, len(d.Lines))
	}
	for i, r := range d.Lines {
		if r["tok"] != nil {
			t.Fatalf("deferred row %d carries runs: %v", i, r)
		}
	}
	var tk lineTokWire
	if code := getJSON(t, ts, url+"&tok=1", &tk); code != 200 {
		t.Fatalf("tok=1: code %d", code)
	}
	if tk.Sum != d.Sum || len(tk.Tok) != len(d.Lines) {
		t.Fatalf("tok=1: sum %q (want %q), %d lines (want %d)", tk.Sum, d.Sum, len(tk.Tok), len(d.Lines))
	}
	ts2, sha2 := bigGoRepo(t)
	var one lineRowsWire
	if code := getJSON(t, ts2, strings.ReplaceAll(base, "SHA", sha2), &one); code != 200 {
		t.Fatalf("one-shot: code %d", code)
	}
	if one.TokPending || one.Sum != "" {
		t.Fatalf("one-shot answer changed: pending=%v sum=%q", one.TokPending, one.Sum)
	}
	for i, l := range tk.Tok {
		got, _ := json.Marshal(l)
		want, _ := json.Marshal(one.Lines[i]["tok"])
		if len(l) == 0 {
			got = []byte("null")
		}
		if string(got) != string(want) {
			t.Fatalf("line %d: late runs %s, one-shot row %s", i+1, got, want)
		}
	}
}

func TestFileContentColoursLater(t *testing.T) {
	t.Parallel()
	checkLineTwoHalves(t, "/api/file-content?src=commit&rev=SHA&path=big.go")
}

func TestBlameColoursLater(t *testing.T) {
	t.Parallel()
	checkLineTwoHalves(t, "/api/blame?path=big.go&rev=SHA")
}

// A small file is coloured at once even when deferred: no second request.
func TestFileContentSmallFileAnswersInlineDeferred(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write(t, dir, "a.go", "package p\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "one")
	ts := serve(t, New(domain.Open(dir)))
	var d lineRowsWire
	if code := getJSON(t, ts, "/api/file-content?src=worktree&path=a.go&defer_tok=1", &d); code != 200 {
		t.Fatalf("code %d", code)
	}
	if d.TokPending || len(d.Lines) == 0 || d.Lines[0]["tok"] == nil {
		t.Fatalf("pending=%v rows=%v; want the runs inline", d.TokPending, d.Lines)
	}
}
