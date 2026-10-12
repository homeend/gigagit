package domain

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/overviewhtml"
)

var exportBlobRE = regexp.MustCompile(`(?s)<script type="application/json" id="gg-data">(.*?)</script>`)

func exportBlob(t *testing.T, page []byte) overviewhtml.Export {
	t.Helper()
	m := exportBlobRE.FindSubmatch(page)
	if m == nil {
		t.Fatal("no data blob")
	}
	var e overviewhtml.Export
	if err := json.Unmarshal(m[1], &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestExportStoredOverview(t *testing.T) {
	t.Parallel()
	svc, _, dir := sendRepo(t)
	rid := savePRReview(t, svc, overviewDoc)
	page, name, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{ReviewID: rid})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".html") || !strings.HasPrefix(name, "review-overview-") {
		t.Errorf("name = %q", name)
	}
	e := exportBlob(t, page)
	if e.Kind != "review" || len(e.Files) != 1 || e.Files[0].Path != "big.go" {
		t.Fatalf("files = %+v", e.Files)
	}
	f := e.Files[0]
	if !f.Changed || len(f.Diff) == 0 || len(f.Lines) < 25 || f.Placeholder != "" {
		t.Errorf("big.go: changed=%v diff=%d lines=%d ph=%q", f.Changed, len(f.Diff), len(f.Lines), f.Placeholder)
	}
	missing := map[string]bool{}
	for _, a := range e.Anchors {
		missing[a.Dest] = a.Missing
	}
	for dest, want := range map[string]bool{"big.go:5": false, "big.go:24-25": false, "big.go": false, "other.go:1": true, "big.go:900": true, "note:t3": true} {
		if got, ok := missing[dest]; !ok || got != want {
			t.Errorf("%s missing = %v (listed %v), want %v", dest, got, ok, want)
		}
	}
	if e.Left == "" || e.Right == "" {
		t.Errorf("range labels empty: %q %q", e.Left, e.Right)
	}
	s := string(page)
	if strings.Contains(s, "check this") { // the review's line note (R3)
		t.Error("a review note leaked into the export")
	}
	if strings.Contains(s, dir) {
		t.Error("the worktree path leaked into the export")
	}
}

func TestExportStoredOverviewAbsent(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, `{"version":1,"summary":"no overview","files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"x"}]}]}`)
	if _, _, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{ReviewID: rid}); !errors.Is(err, ErrNoOverview) {
		t.Fatalf("err = %v, want ErrNoOverview", err)
	}
}

func TestExportAgentOverview(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	commitFile(t, dir, "a.txt", "one\ntwo\nthree\n", "add a")
	commitFile(t, dir, "img.bin", "\x00\x01\x02", "add bin")
	commitFile(t, dir, "c.txt", "same\n", "add c")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text := "Look at [two](a.txt:2), [the note](note:t1), [gone](nope.txt:1), [bin](img.bin), [far](a.txt:40), [lost note](note:t9), [c](c.txt)."
	page, name, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{
		Title: "My investigation", Text: text,
		NoteSpans: map[string]NoteSpan{"t1": {Path: "a.txt", Start: 3, End: 4}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "my-investigation.html" {
		t.Errorf("name = %q", name)
	}
	e := exportBlob(t, page)
	if e.Kind != "agent" || e.Left != "HEAD" || e.Right != "working tree" {
		t.Errorf("kind/range = %q %q %q", e.Kind, e.Left, e.Right)
	}
	got := map[string]overviewhtml.Anchor{}
	for _, a := range e.Anchors {
		got[a.Dest] = a
	}
	if a := got["note:t1"]; a.Missing || a.Path != "a.txt" || a.Start != 3 || a.End != 4 {
		t.Errorf("note anchor = %+v", a)
	}
	if !got["nope.txt:1"].Missing || !got["a.txt:40"].Missing || !got["note:t9"].Missing || got["a.txt:2"].Missing || got["img.bin"].Missing {
		t.Errorf("missing flags = %+v", got)
	}
	if len(e.Files) != 3 || e.Files[0].Path != "a.txt" || !e.Files[0].Changed || len(e.Files[0].Lines) != 4 || len(e.Files[0].Diff) == 0 {
		t.Fatalf("files = %+v", e.Files)
	}
	if e.Files[1].Path != "img.bin" || e.Files[1].Placeholder == "" || e.Files[1].Lines != nil {
		t.Errorf("img.bin must be a placeholder: %+v", e.Files[1])
	}
	if c := e.Files[2]; c.Path != "c.txt" || c.Changed || c.Diff != nil || len(c.Lines) != 1 {
		t.Errorf("c.txt must be unchanged with no diff (D says \"unchanged in this range\"): %+v", c)
	}
}

func TestAgentOverviewSourceResolvesNotes(t *testing.T) {
	t.Parallel()
	docs := agentdocs.New()
	n, err := docs.AddNote("r", "a.txt", []string{"one", "two", "three"}, 2, 3, "look", "", "")
	if err != nil {
		t.Fatal(err)
	}
	o, err := docs.AddOverview("r", "/w", "Tour", "[n](note:"+n.ID+") and [a](a.txt:1)")
	if err != nil {
		t.Fatal(err)
	}
	src, err := AgentOverviewSource(docs, o.ID, "r")
	if err != nil || src.Title != "Tour" || src.Text != o.Text {
		t.Fatalf("src = %+v %v", src, err)
	}
	if got := src.NoteSpans[n.ID]; got != (NoteSpan{Path: "a.txt", Start: 2, End: 3}) {
		t.Errorf("note span = %+v", got)
	}
	if _, err := AgentOverviewSource(docs, o.ID, "other"); !errors.Is(err, ErrNoAgentOverview) {
		t.Errorf("another root: %v", err)
	}
	if _, err := AgentOverviewSource(docs, "f999999", "r"); !errors.Is(err, ErrNoAgentOverview) {
		t.Errorf("unknown id: %v", err)
	}
}
