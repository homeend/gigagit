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

// The user opened file X's preview, then file Y's: X's late load is stale
// and must not overwrite Y.
func TestStalePreviewLoadIsDropped(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m, _ = m.openPreviewSrc(fileSource{kind: srcCommit, rev: "aaa"}, "x.txt", nil)
	x := m.filesPreview
	m, _ = m.openPreviewSrc(fileSource{kind: srcCommit, rev: "aaa"}, "y.txt", nil)
	tm, _ := m.Update(fileContentMsg{tag: x.tag, lines: fileContentLinesTok([]byte("XXX\n"), nil)})
	m = tm.(Model)
	if l := m.filesPreview.p.lines[0]; l.src || m.filesPreview.path != "y.txt" {
		t.Fatalf("preview = %s %+v, want y.txt still loading", m.filesPreview.path, l)
	}
	tm, _ = m.Update(fileContentMsg{tag: m.filesPreview.tag, lines: fileContentLinesTok([]byte("YYY\n"), nil)})
	if l := tm.(Model).filesPreview.p.lines[0]; l.raw != "YYY" {
		t.Fatalf("the preview's own load did not fill it: %+v", l)
	}
}

// A reload of a file the user is reading keeps their place.
func TestOpenFileReloadKeepsPlace(t *testing.T) {
	t.Parallel()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f")
	d.fill(fileContentMsg{tag: d.tag, lines: docLines(60)}, 10, 40)
	d.p.cur, d.p.sel = 33, 28
	d.keepPlace()
	if n := d.fill(fileContentMsg{tag: d.tag, lines: docLines(60)}, 10, 40); n != "" || d.p.cur != 33 || d.p.sel != 28 {
		t.Fatalf("cur=%d sel=%d notice=%q, want 33/28 and none", d.p.cur, d.p.sel, n)
	}
	// The file shrank: the place clamps into it.
	d.keepPlace()
	d.fill(fileContentMsg{tag: d.tag, lines: docLines(20)}, 10, 40)
	if d.p.cur != 19 || d.p.sel != 10 {
		t.Fatalf("after shrink cur=%d sel=%d, want 19/10", d.p.cur, d.p.sel)
	}
	// A line the link asks for wins over the kept place.
	d.keepPlace()
	d.pendingLine = 5
	d.fill(fileContentMsg{tag: d.tag, lines: docLines(20)}, 10, 40)
	if d.p.cur != 4 {
		t.Fatalf("cur=%d, want 4 (the asked line)", d.p.cur)
	}
	// A kept place is used once.
	d.p.cur = 7
	d.fill(fileContentMsg{tag: d.tag, lines: docLines(20)}, 10, 40)
	if d.p.cur != 0 {
		t.Fatalf("cur=%d, want 0 (no place kept)", d.p.cur)
	}
}
