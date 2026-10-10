package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"strings"
	"testing"
)

// In the files view's preview: v / l / space / j j / end / enter copies the
// source text across lines — the empty line kept — and the line cursor
// follows the character cursor.
func TestPreviewCharSelCopies(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t) // "\tindented", "", "line000", …
	p := m.filesPreview.p
	var got string
	m = captureClip(m, &got)
	m = feedPreview(m, "v")
	if !p.cs.on || p.cs.cur != (pos{0, 0}) {
		t.Fatalf("v: %+v", p.cs)
	}
	m = feedPreview(m, "l", "space", "j", "j", "end")
	if p.cur != 2 {
		t.Fatalf("the line cursor must follow: cur %d, want 2", p.cur)
	}
	if !strings.Contains(m.View(), "[enter y] copy") {
		t.Fatal("the bottom bar must show the mode's keys")
	}
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if got != "indented\n\nline000" || p.cs.on {
		t.Fatalf("copied %q (on=%v)", got, p.cs.on)
	}
}

// The stripe covers a tab's four display cells; the cursor sits after them.
func TestPreviewCharSelPaintsTheTab(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview.p
	m = feedPreview(m, "v", "space", "l")
	mask := p.previewCharEmph(nil, 0, p.lines[0].text)
	if len(mask) < 5 || mask[0] != emphSel || mask[3] != emphSel || mask[4] != emphSelCur || mask[5] != emphNone {
		t.Fatalf("mask %v, want the tab's 4 cells striped and the cursor on i", mask)
	}
	_ = m
}

// The mode is modal: space, / and esc's close are its keys; v clears a
// live line selection.
func TestPreviewCharSelIsModal(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview.p
	m = feedPreview(m, "space", "v")
	if p.lsel.on || !p.cs.on {
		t.Fatalf("v must replace the line selection: lsel %+v cs %+v", p.lsel, p.cs)
	}
	m = feedPreview(m, "/")
	if p.search.typing {
		t.Fatal("/ must not start a search in the mode")
	}
	m = feedPreview(m, "esc")
	if p.cs.on || m.filesPreview == nil {
		t.Fatal("esc leaves the mode and keeps the preview")
	}
}

// Cutoff mode runs as scroll while the mode is on (the code pans with the
// cursor); leaving puts cutoff back.
func TestPreviewCharSelScrollsWhileOnInCutoffMode(t *testing.T) {
	t.Parallel()
	m := openPreview(t, fullTreeTreeSideOf(t, previewModelN(strings.Repeat("x", 300)+"\nshort\n")))
	p := m.filesPreview.p
	p.mode = modeCutoff
	m = feedPreview(m, "v")
	if p.mode != modeScroll {
		t.Fatalf("v in cutoff: mode %v, want scroll", p.mode)
	}
	m = feedPreview(m, "end")
	if p.hscroll == 0 {
		t.Fatal("end must pan to the cursor")
	}
	m = feedPreview(m, "esc")
	if p.cs.on || p.mode != modeCutoff || p.hscroll != 0 {
		t.Fatalf("after esc: on=%v mode %v hscroll %d", p.cs.on, p.mode, p.hscroll)
	}
}

// A load arriving mid-mode replaces the lines: the mode leaves.
func TestPreviewCharSelLeavesOnALoad(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	m = feedPreview(m, "v", "space", "l")
	u, _ := m.Update(fileContentMsg{tag: m.filesPreview.tag, lines: fileContentLines([]byte("a\nb\n"))})
	m = u.(Model)
	if m.filesPreview.p.cs.on {
		t.Fatal("a load must leave the mode")
	}
}

// A placeholder ("(loading…)") is no line of the file: nothing to select.
func TestPreviewCharSelOnAPlaceholderSaysSo(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview.p
	p.lines = []contentLine{{text: "(loading…)"}}
	m = feedPreview(m, "v")
	if p.cs.on || m.statusMsg != "▸ nothing to select" {
		t.Fatalf("on=%v status %q", p.cs.on, m.statusMsg)
	}
}

// The full-screen viewer hosts the mode too, its hint line showing the keys.
func TestViewerCharSelCopies(t *testing.T) {
	t.Parallel()
	m, fv := viewerAt(t, "two.txt", "one\ntwo\n", 0)
	var got string
	m = captureClip(m, &got)
	m = feedPreview(m, "v", "space", "j", "end")
	if !fv.p.cs.on || !strings.Contains(m.View(), "[enter y] copy 6 chars") {
		t.Fatalf("on=%v, the viewer's hint must show the mode's keys", fv.p.cs.on)
	}
	u, cmd := m.Update(keyMsg("y"))
	drainCmds(t, u.(Model), cmd)
	if got != "one\ntwo" {
		t.Fatalf("copied %q", got)
	}
}

const charSelOverviewText = "# Tour\n\nThis paragraph of the overview is long enough to wrap inside the viewer's reading column more than once, so that a copy across its rows must join them with one space and never with a newline.\n"

// An overview document is laid out by gg itself: the mode keeps its view (no
// switch, which would re-lay it) and a copy across a wrapped row joins the
// rows with one space.
func TestViewerCharSelJoinsAnOverviewsWrappedRows(t *testing.T) {
	t.Parallel()
	m, d := shownOverview(t, charSelOverviewText)
	p := d.p
	mode := p.mode
	var got string
	m = captureClip(m, &got)
	p.cur = 0
	m = feedPreview(m, "v")
	if !p.cs.on || p.mode != mode {
		t.Fatalf("v: on=%v mode %v (was %v)", p.cs.on, p.mode, mode)
	}
	start := -1
	for i, l := range p.lines {
		if strings.HasPrefix(l.raw, "This paragraph") {
			start = i
		}
	}
	if start < 0 || start+1 >= len(p.lines) || !p.lines[start+1].cont {
		t.Fatalf("the paragraph must wrap: %q", previewTexts(p.lines))
	}
	for p.cs.cur.row < start {
		m = feedPreview(m, "j")
	}
	m = feedPreview(m, "end", "space", "j", "home")
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	first := []rune(p.lines[start].raw)
	next := []rune(p.lines[start+1].raw)
	if want := string(first[len(first)-1]) + " " + string(next[p.lines[start+1].lead]); got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
}

func previewTexts(lines []contentLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.raw
	}
	return out
}

// A prose document in the full-screen viewer is drawn in the reading
// column, not the frame's full width: the mode's pan keeps the cursor inside
// THAT column (a long code line in an overview, which opens in scroll).
func TestViewerCharSelPansInsideTheReadingColumn(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("y", 170)
	m, d := shownOverview(t, "# Tour\n\n```\n"+long+"\n```\n")
	m.width, m.height = 200, 40
	_ = m.View() // lay the overview out at this width
	p := d.p
	m = feedPreview(m, "v")
	for i := 0; i < len(p.lines) && !strings.HasPrefix(p.lines[p.cs.cur.row].raw, "y"); i++ {
		m = feedPreview(m, "j")
	}
	m = feedPreview(m, "end")
	_, inner := m.viewerGeom()
	col, _ := readingColumn(inner, m.readingWidth())
	cell := previewDispCol(p.csRows[p.cs.cur.row].text, p.lines[p.cs.cur.row].lead, p.cs.cur.col)
	if cell < p.hscroll || cell >= p.hscroll+col {
		t.Fatalf("cursor cell %d is outside the reading column [%d, %d)", cell, p.hscroll, p.hscroll+col)
	}
}

// A click on the tree takes the keys from the preview: the mode leaves with
// them (it would otherwise paint on, swallowing nothing, and resurface when
// the preview is focused again), and the reader's view comes back.
func TestPreviewCharSelLeavesWhenTheTreeTakesFocus(t *testing.T) {
	t.Parallel()
	m := previewTabModel(t)
	p := m.filesPreview.p
	p.mode = modeCutoff
	m = feedPreview(m, "v")
	u, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 3, Y: 4})
	m = u.(Model)
	if !m.filesTreeFocused {
		t.Fatal("the click must focus the tree")
	}
	if p.cs.on || p.mode != modeCutoff {
		t.Fatalf("after the tree took focus: on=%v mode %v", p.cs.on, p.mode)
	}
}

// A document put aside (detached: a navigate, another viewer on top of it)
// leaves the mode, so it never comes back already in it.
func TestViewerCharSelLeavesWhenTheDocumentIsDetached(t *testing.T) {
	t.Parallel()
	m, fv := viewerAt(t, "two.txt", "one\ntwo\n", 0)
	m = feedPreview(m, "v", "space")
	m = m.detachDoc(fv.openFile)
	if fv.p.cs.on {
		t.Fatal("a detached document must leave the mode")
	}
}

// An overview in the cut view: a cut row reads "prefix…", so the mode
// switches the document to scroll (re-laying it) before it starts — the
// copy is the whole line, never the ellipsis — and leaving puts cut back.
func TestViewerCharSelOverviewCutViewScrollsWhileOn(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("z", 170)
	m, d := shownOverview(t, "# Tour\n\n```\n"+long+"\n```\n")
	p := d.p
	for p.mode != modeCutoff {
		m = feedPreview(m, "ctrl+w")
		_ = m.View()
	}
	var got string
	m = captureClip(m, &got)
	m = feedPreview(m, "v")
	if !p.cs.on || p.mode != modeScroll {
		t.Fatalf("v in the cut view: on=%v mode %v, want scroll", p.cs.on, p.mode)
	}
	for i := 0; i < len(p.lines) && !strings.Contains(p.lines[p.cs.cur.row].raw, "z"); i++ {
		m = feedPreview(m, "j")
	}
	m = feedPreview(m, "space", "end")
	u, cmd := m.Update(keyMsg("enter"))
	m = drainCmds(t, u.(Model), cmd)
	if strings.TrimSpace(got) != long {
		t.Fatalf("copied %q, want the whole line", got)
	}
	_ = m.View()
	if p.mode != modeCutoff {
		t.Fatalf("after the copy the view is %v, want cut back", p.mode)
	}
}

// An indented row (a code block inside a list item) pans on its display
// cells, lead included: at the row's end the cursor sits in the column's
// last cell — not `lead` cells short of it — and home pans back to 0.
func TestViewerCharSelPansAnIndentedRowToTheLastCell(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("y", 170)
	m, d := shownOverview(t, "# Tour\n\n- item\n\n  ```\n  "+long+"\n  ```\n")
	m.width, m.height = 200, 40
	_ = m.View()
	p := d.p
	p.mode = modeScroll
	rows, _ := m.viewerGeom()
	d.layOut(rows, d.ov.w)
	m = feedPreview(m, "v")
	for i := 0; i < len(p.lines) && !strings.Contains(p.lines[p.cs.cur.row].raw, "yyy"); i++ {
		m = feedPreview(m, "j")
	}
	lead := p.lines[p.cs.cur.row].lead
	if lead == 0 {
		t.Fatalf("the code row must hang behind the item's lead: %q", previewTexts(p.lines))
	}
	m = feedPreview(m, "end")
	_, inner := m.viewerGeom()
	col, _ := readingColumn(inner, m.readingWidth())
	col = max(col-d.gutterW(), 1)
	cell := previewDispCol(p.csRows[p.cs.cur.row].text, lead, p.cs.cur.col)
	if cell != p.hscroll+col-1 {
		t.Fatalf("cursor cell %d, hscroll %d, column %d: want the last cell %d", cell, p.hscroll, col, p.hscroll+col-1)
	}
	m = feedPreview(m, "home")
	if p.hscroll != 0 {
		t.Fatalf("home: hscroll %d", p.hscroll)
	}
	_ = m
}
