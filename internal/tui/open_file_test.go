package tui

import (
	"fmt"
	"strings"
	"testing"
)

func docLines(n int) []contentLine {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return fileContentLinesTok([]byte(b.String()), nil)
}

func TestOpenFileTagsAreUnique(t *testing.T) {
	t.Parallel()
	a := newOpenFile(fileSource{kind: srcWorktree}, "f.go")
	b := newOpenFile(fileSource{kind: srcWorktree}, "f.go")
	if a.tag == b.tag {
		t.Fatalf("two documents share the tag %q", a.tag)
	}
	if a.key() != b.key() {
		t.Fatalf("keys %q / %q differ for one file", a.key(), b.key())
	}
	c := newOpenFile(fileSource{kind: srcCommit, rev: "abc"}, "f.go")
	if c.key() == a.key() {
		t.Fatalf("a commit version and the working tree share the key %q", c.key())
	}
	if len(a.p.lines) != 1 || a.p.lines[0].src {
		t.Fatalf("a new document shows %+v, want the loading placeholder", a.p.lines)
	}
}

func TestOpenFileFillSetsLinesAndResetsCursor(t *testing.T) {
	t.Parallel()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f")
	d.p.cur, d.p.sel = 5, 3
	d.p.lsel.on = true
	if n := d.fill(fileContentMsg{tag: d.tag, lines: docLines(10)}, 5, 40); n != "" {
		t.Errorf("notice %q, want none", n)
	}
	if len(d.p.lines) != 10 || d.p.cur != 0 || d.p.sel != 0 || d.p.lsel.on {
		t.Fatalf("after fill: %d lines cur=%d sel=%d lsel=%v", len(d.p.lines), d.p.cur, d.p.sel, d.p.lsel.on)
	}
}

func TestOpenFileFillError(t *testing.T) {
	t.Parallel()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f")
	d.fill(fileContentMsg{tag: d.tag, err: fmt.Errorf("boom")}, 5, 40)
	if len(d.p.lines) != 1 || !strings.Contains(d.p.lines[0].text, "boom") {
		t.Fatalf("lines = %+v, want the load-failed line", d.p.lines)
	}
}

func TestOpenFileFillLandsPendingLine(t *testing.T) {
	t.Parallel()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f")
	d.pendingLine = 40
	d.fill(fileContentMsg{tag: d.tag, lines: docLines(60)}, 10, 40)
	if d.p.cur != 39 || d.p.cur < d.p.sel || d.p.cur >= d.p.sel+10 {
		t.Fatalf("cur=%d sel=%d, want cur 39 inside the window", d.p.cur, d.p.sel)
	}
	if d.pendingLine != 0 {
		t.Errorf("pendingLine = %d, want it consumed", d.pendingLine)
	}
}

func TestOpenFileFillPastEOFNotice(t *testing.T) {
	t.Parallel()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f")
	d.pendingLine = 99
	n := d.fill(fileContentMsg{tag: d.tag, lines: docLines(5)}, 10, 40)
	if d.p.cur != 4 || n != "line 99 is past the end of f (5 lines)" {
		t.Fatalf("cur=%d notice=%q", d.p.cur, n)
	}
}

func TestOpenFileFillPlaceholderIgnoresLine(t *testing.T) {
	t.Parallel()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f")
	d.pendingLine = 3
	n := d.fill(fileContentMsg{tag: d.tag, lines: fileContentLinesTok(nil, nil)}, 10, 40)
	if d.p.cur != 0 || n != "" {
		t.Fatalf("cur=%d notice=%q, want 0 and none", d.p.cur, n)
	}
}

// A search typed while "(loading…)" showed is re-run over the loaded lines
// and scrolls its hit into view.
func TestOpenFileFillRefindsSearch(t *testing.T) {
	t.Parallel()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f")
	d.p.search.query = "line 45"
	d.fill(fileContentMsg{tag: d.tag, lines: docLines(60)}, 10, 40)
	if len(d.p.search.hits) != 1 {
		t.Fatalf("hits = %v, want one", d.p.search.hits)
	}
	if r := d.p.search.hits[0].row; r < d.p.sel || r >= d.p.sel+10 {
		t.Fatalf("hit row %d outside the window from %d", r, d.p.sel)
	}
}
