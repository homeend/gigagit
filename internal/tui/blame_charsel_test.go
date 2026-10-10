package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// charBlameModel is a blame of two lines — one with a tab, one ending in a
// CRLF's \r — with the cursor on the first.
func charBlameModel(contents ...string) (Model, *blameView) {
	m := Model{width: 100, height: 30}
	b := &blameView{ctx: navContext{path: "a.go"}, tag: "blame::a.go"}
	for i, c := range contents {
		b.lines = append(b.lines, model.BlameLine{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: i + 1, Content: c})
	}
	b.blocks = groupBlame(b.lines)
	m = m.pushLayer(b)
	return m, b
}

// v / l / space / j / end / enter copies the source text — the tab raw, the
// \r never — and the line cursor follows the character cursor.
func TestBlameCharSelCopies(t *testing.T) {
	t.Parallel()
	m, b := charBlameModel("x\tyz", "second\r")
	var got string
	m = captureClip(m, &got)
	m = typeBlame(m, b, "v")
	if !b.cs.on || b.cs.cur != (pos{0, 0}) {
		t.Fatalf("v: %+v", b.cs)
	}
	m = typeBlame(m, b, "l", "space", "j", "end")
	if b.sel != 1 || b.cs.cur.col != 5 {
		t.Fatalf("j end: line cursor %d, column %d — want 1 and 5 (d, not the \\r)", b.sel, b.cs.cur.col)
	}
	m, cmd := b.update(m, keyMsg("enter"))
	drainCmds(t, m, cmd)
	if got != "\tyz\nsecond" || b.cs.on {
		t.Fatalf("copied %q (on=%v)", got, b.cs.on)
	}
}

// The mode is modal: / and enter's history and space's line selection are
// the mode's keys while it is on; v clears a live line selection.
func TestBlameCharSelIsModal(t *testing.T) {
	t.Parallel()
	m, b := charBlameModel("one", "two")
	m = typeBlame(m, b, "space")
	m = typeBlame(m, b, "v")
	if b.lsel.on || !b.cs.on {
		t.Fatalf("v must replace the line selection: lsel %+v cs %+v", b.lsel, b.cs)
	}
	m = typeBlame(m, b, "/", "o")
	if b.search.typing || b.search.active() {
		t.Fatal("/ must not start a search in the mode")
	}
	if _, ok := m.topLayer().(*blameView); !ok {
		t.Fatalf("top layer %T", m.topLayer())
	}
	m = typeBlame(m, b, "esc")
	if b.cs.on {
		t.Fatal("esc leaves the mode")
	}
}

// The stripe covers a tab's every cell and paints the code, never the
// gutter: the row mask is in display runes.
func TestBlameCharSelPaintsTheCode(t *testing.T) {
	t.Parallel()
	m, b := charBlameModel("x\tyz", "two")
	m = typeBlame(m, b, "v", "space", "l", "l")
	mask := b.charEmphOn(0, "x   yz")
	want := []emphLevel{emphSel, emphSel, emphSel, emphSel, emphSelCur, emphNone}
	if !equalEmph(mask, want) {
		t.Fatalf("mask %v, want %v", mask, want)
	}
	if !strings.Contains(b.render(m, ""), "[enter y] copy 3 chars") {
		t.Fatal("the footer must show the mode's hint")
	}
}

// In cutoff mode a long line is cut: the mode scrolls while it is on (the
// code pans with the cursor) and puts cutoff back when it ends.
func TestBlameCharSelScrollsWhileOnInCutoffMode(t *testing.T) {
	t.Parallel()
	m, b := charBlameModel(strings.Repeat("x", 300), "y")
	b.mode = modeCutoff
	m = typeBlame(m, b, "v")
	if b.mode != modeScroll {
		t.Fatalf("v in cutoff: mode %v, want scroll", b.mode)
	}
	m = typeBlame(m, b, "end")
	if b.hscroll == 0 {
		t.Fatal("end must pan to the cursor")
	}
	m = typeBlame(m, b, "esc")
	if b.cs.on || b.mode != modeCutoff || b.hscroll != 0 {
		t.Fatalf("after esc: on=%v mode %v hscroll %d", b.cs.on, b.mode, b.hscroll)
	}
	_ = m
}

// A reload replaces the lines the positions index: the mode leaves, and the
// reader's display mode comes back.
func TestBlameCharSelLeavesOnReload(t *testing.T) {
	t.Parallel()
	m, b := charBlameModel("one", "two")
	b.mode = modeCutoff
	m = typeBlame(m, b, "v", "space")
	u, _ := m.Update(blameMsg{tag: b.tag, lines: []model.BlameLine{{Hash: "b", LineNo: 1, Content: "new"}}})
	m = u.(Model)
	if b.cs.on || b.mode != modeCutoff {
		t.Fatalf("after a reload: on=%v mode %v", b.cs.on, b.mode)
	}
}

// Nothing to select: a loading blame says so.
func TestBlameCharSelOnALoadingBlameSaysSo(t *testing.T) {
	t.Parallel()
	m, b := charBlameModel()
	b.loading = true
	m = typeBlame(m, b, "v")
	if b.cs.on || m.statusMsg != "▸ nothing to select" {
		t.Fatalf("on=%v status %q", b.cs.on, m.statusMsg)
	}
}

// The line selection and Copy line drop a CRLF line's \r too.
func TestBlameLineCopiesDropTheCarriageReturn(t *testing.T) {
	t.Parallel()
	m, b := charBlameModel("ab\r", "cd\r")
	if row, ok := rowByID(m.contextCopyRows(), "copy-line"); !ok || row.copyText != "ab" {
		t.Fatalf("Copy line = %q", row.copyText)
	}
	m = typeBlame(m, b, "space", "down")
	if row, ok := rowByID(m.contextCopyRows(), "copy-selected-lines"); !ok || row.copyText != "ab\ncd" {
		t.Fatalf("Copy selected lines = %q", row.copyText)
	}
}

// Blame covers the status bar: the copy's confirmation (and the mode's
// notices) ride the header line, as the file viewer's title line does.
func TestBlameShowsTheStatusOnItsHeader(t *testing.T) {
	t.Parallel()
	m, b := charBlameModel("one", "two")
	var got string
	m = captureClip(m, &got)
	m = typeBlame(m, b, "v", "space", "l")
	m, cmd := b.update(m, keyMsg("enter"))
	m = drainCmds(t, m, cmd)
	head := strings.SplitN(b.render(m, ""), "\n", 2)[0]
	if got != "on" || !strings.Contains(head, "Copied 2 characters") {
		t.Fatalf("copied %q; header %q", got, head)
	}
	u, _ := m.Update(keyMsg("j"))
	if head := strings.SplitN(b.render(u.(Model), ""), "\n", 2)[0]; strings.Contains(head, "Copied") {
		t.Fatalf("the next key must clear it: header %q", head)
	}
}
