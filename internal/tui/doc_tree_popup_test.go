package tui

import (
	"strings"
	"testing"
)

// treePopup opens ctrl+\ on the tree tab of treeModel.
func treePopup(t *testing.T) (Model, *sessionsPopup, *openFile) {
	t.Helper()
	m, ov := treeModel(t)
	m, _ = m.openSessionsPopup(false)
	p, ok := m.topLayer().(*sessionsPopup)
	if !ok {
		t.Fatalf("top = %T", m.topLayer())
	}
	if p.tab != tabFiles {
		t.Fatalf("tab = %d, want the tree", p.tab)
	}
	return m, p, ov
}

// selectRow puts the cursor on the first row containing text.
func selectTreeRow(t *testing.T, p *sessionsPopup, text string) {
	t.Helper()
	for i, r := range p.rows {
		if strings.Contains(r, text) {
			p.sel = i
			return
		}
	}
	t.Fatalf("no row with %q in\n%s", text, strings.Join(p.rows, "\n"))
}

func TestTreeTabRowsStartCollapsed(t *testing.T) {
	t.Parallel()
	m, p, _ := treePopup(t)
	want := []string{"▸ ○ Tour  overview · 5 anchors", "▸ Notes  1 file", "▸ Opened  1 file"}
	if len(p.rows) != len(want) {
		t.Fatalf("rows:\n%s", strings.Join(p.rows, "\n"))
	}
	for i, w := range want {
		if !strings.HasPrefix(p.rows[i], w) {
			t.Errorf("row %d = %q, want prefix %q", i, p.rows[i], w)
		}
	}
	if v := m.View(); !strings.Contains(v, "[Overviews & notes 4]") || strings.Contains(v, "Open files") {
		t.Errorf("tab strip:\n%s", v)
	}
}

func TestTreeTabRightLeftExpandAndCollapse(t *testing.T) {
	t.Parallel()
	m, p, _ := treePopup(t)
	selectTreeRow(t, p, "Tour")
	m, _ = p.update(m, keyMsg("right"))
	joined := strings.Join(p.rows, "\n")
	for _, w := range []string{"▾ ○ Tour", "    ○ a.txt", "    ○ b.txt", "    ○ c.txt  :1  1 note  working tree"} {
		if !strings.Contains(joined, w) {
			t.Fatalf("expanded rows lack %q:\n%s", w, joined)
		}
	}
	selectTreeRow(t, p, "b.txt")
	m, _ = p.update(m, keyMsg("left"))
	if !strings.HasPrefix(p.rows[p.sel], "▸ ○ Tour") || strings.Contains(strings.Join(p.rows, "\n"), "b.txt") {
		t.Fatalf("left on a child: sel %d rows\n%s", p.sel, strings.Join(p.rows, "\n"))
	}
	selectTreeRow(t, p, "Notes")
	m, _ = p.update(m, keyMsg("enter")) // enter on a group toggles it
	selectTreeRow(t, p, "d.txt")
	m, _ = p.update(m, keyMsg("left"))
	if !strings.HasPrefix(p.rows[p.sel], "▸ Notes") {
		t.Fatalf("left on a group file: sel on %q", p.rows[p.sel])
	}
	_ = m
}

// The expand state outlives the popup (it is the registry's), and the cursor
// stays on its node when the list is rebuilt.
func TestTreeTabStateOutlivesThePopupAndKeepsTheCursor(t *testing.T) {
	t.Parallel()
	m, p, _ := treePopup(t)
	selectTreeRow(t, p, "Opened")
	m, _ = p.update(m, keyMsg("right"))
	m, _ = p.update(m, keyMsg("esc"))
	m, _ = m.openSessionsPopup(false)
	p = m.topLayer().(*sessionsPopup)
	if !strings.Contains(strings.Join(p.rows, "\n"), "e.txt") {
		t.Fatalf("Opened did not stay expanded:\n%s", strings.Join(p.rows, "\n"))
	}
	selectTreeRow(t, p, "e.txt")
	selectTreeRow(t, p, "Tour")
	m, _ = p.update(m, keyMsg("right")) // rows above e.txt grow
	selectTreeRow(t, p, "e.txt")
	before := p.rows[p.sel]
	m, _ = m.onAgentDocsChanged()
	p.refresh(m)
	if p.rows[p.sel] != before {
		t.Fatalf("cursor moved to %q", p.rows[p.sel])
	}
}

func TestTreeTabEnterOnAnOverviewOpensTheOverview(t *testing.T) {
	t.Parallel()
	m, p, ov := treePopup(t)
	selectTreeRow(t, p, "Tour")
	m, cmd := p.update(m, keyMsg("enter"))
	m = pumpAll(t, m, cmd)
	if !m.docShown(ov) {
		t.Fatalf("the overview is not on screen (top %T)", m.topLayer())
	}
}

func TestTreeTabXClosesAnOpenChild(t *testing.T) {
	t.Parallel()
	m, p, _ := treePopup(t)
	selectTreeRow(t, p, "Tour")
	m, _ = p.update(m, keyMsg("right"))
	selectTreeRow(t, p, "c.txt")
	m, _ = p.update(m, keyMsg("x"))
	if m.openFiles.find(m.currentWorktree, docKey(fileSource{kind: srcWorktree}, "c.txt")) != nil && !strings.Contains(m.statusMsg, "c.txt") {
		t.Fatalf("x on an open child did nothing (status %q)", m.statusMsg)
	}
	selectTreeRow(t, p, "a.txt") // not open: x does nothing
	m, _ = p.update(m, keyMsg("x"))
	_ = m
}

// enter on an overview's file opens the overview with that file's first
// anchor selected, the file beside it; again on another file while that
// split is up steers the same split there.
func TestTreeTabEnterOnAChildOpensTheOverviewAtThatFile(t *testing.T) {
	t.Parallel()
	m, p, ov := treePopup(t)
	m.width, m.height = 160, 40
	selectTreeRow(t, p, "Tour")
	m, _ = p.update(m, keyMsg("right"))
	selectTreeRow(t, p, "b.txt")
	m, cmd := p.update(m, keyMsg("enter"))
	m = pumpAll(t, m, cmd)
	sv, ok := m.topLayer().(*splitView)
	if !ok || sv.left.doc() != ov || ov.ov.sel != 1 {
		t.Fatalf("top %T sel %d, want the split on the overview at anchor 1", m.topLayer(), ov.ov.sel)
	}
	if rd := sv.right.doc(); rd == nil || rd.path != "b.txt" {
		t.Fatalf("right = %v, want b.txt", sv.right.doc())
	}
	m, _ = m.openSessionsPopup(false)
	p = m.topLayer().(*sessionsPopup)
	selectTreeRow(t, p, "c.txt")
	m, cmd = p.update(m, keyMsg("enter"))
	m = pumpAll(t, m, cmd)
	sv, ok = m.topLayer().(*splitView)
	if !ok || ov.ov.sel != 2 {
		t.Fatalf("top %T sel %d, want the split at anchor 2", m.topLayer(), ov.ov.sel)
	}
	if rd := sv.right.doc(); rd == nil || rd.path != "c.txt" {
		t.Fatalf("right = %v, want c.txt", sv.right.doc())
	}
}

// Review fix 1: the overview's own cursor follows the anchor enter chose —
// fresh split, split already up, and the narrow full-screen viewer — on a
// long overview whose anchors sit far apart.
func TestTreeTabEnterOnAChildMovesTheOverviewCursorToItsAnchor(t *testing.T) {
	t.Parallel()
	for _, width := range []int{160, 50} {
		m := privateDocsModel(t)
		m.width, m.height = width, 40
		ov := storeOverview(t, m, "Long", "[a](a.txt:1)\n\n"+strings.Repeat("filler line\n\n", 80)+"[c](a.txt:30)\n\n"+strings.Repeat("more\n\n", 40)+"[d](b.txt:2)\n")
		rows, inner := m.viewerGeom()
		ov.layOut(rows, m.overviewWidth(inner))
		m = m.registerDoc(ov)
		m.openFiles.setExpanded(m.currentWorktree, docNode{kind: nodeOverview, doc: ov}.expandKey(), true)
		for _, step := range []struct {
			path string
			want int
		}{{"b.txt", 2}, {"a.txt", 0}} {
			if _, ok := m.topLayer().(*sessionsPopup); !ok {
				m, _ = m.openSessionsPopup(false)
			}
			p := m.topLayer().(*sessionsPopup)
			selectTreeRow(t, p, step.path)
			m2, cmd := p.update(m, keyMsg("enter"))
			m = pumpAll(t, m2, cmd)
			a := ov.ov.anchors[step.want]
			if ov.ov.sel != step.want || len(a.spans) == 0 || ov.p.cur != a.spans[0].line {
				t.Fatalf("width %d %s: sel %d cur %d, want anchor %d on line %v", width, step.path, ov.ov.sel, ov.p.cur, step.want, a.spans)
			}
		}
	}
}

// Review fix 2: under a filter the tree is the filter's: → / ← / enter on a
// group change nothing saved; ← from a child only moves to its overview.
func TestTreeTabKeysUnderAFilterSaveNothing(t *testing.T) {
	t.Parallel()
	m, p, ov := treePopup(t)
	key := docNode{kind: nodeOverview, doc: ov}.expandKey()
	m.openFiles.setExpanded(m.currentWorktree, key, true)
	p.query = "b.tx"
	p.refresh(m)
	selectTreeRow(t, p, "b.txt")
	m, _ = p.update(m, keyMsg("left"))
	if !strings.HasPrefix(p.rows[p.sel], "▾ ○ Tour") {
		t.Fatalf("left under a filter: cursor on %q", p.rows[p.sel])
	}
	m, _ = p.update(m, keyMsg("left"))
	if !m.openFiles.expanded(m.currentWorktree, key) {
		t.Fatal("left under a filter saved a collapse")
	}
	p.query = "e.tx"
	p.refresh(m)
	selectTreeRow(t, p, "Opened")
	m, _ = p.update(m, keyMsg("enter"))
	if m.openFiles.expanded(m.currentWorktree, docNode{kind: nodeGroup, group: groupOpened}.expandKey()) {
		t.Fatal("enter on a group under a filter saved a toggle")
	}
}

// Review fix 3: x on a group's file leaves the cursor on the next file of
// the same group, as the flat list did.
func TestTreeTabXKeepsTheCursorInItsGroup(t *testing.T) {
	t.Parallel()
	m, p, _ := treePopup(t)
	wt := fileSource{kind: srcWorktree}
	bgDoc(m, wt, "f.txt", 2)
	bgDoc(m, wt, "g.txt", 2)
	m.openFiles.setExpanded(m.currentWorktree, docNode{kind: nodeGroup, group: groupOpened}.expandKey(), true)
	p.refresh(m)
	selectTreeRow(t, p, "f.txt")
	at := p.sel
	m, _ = p.update(m, keyMsg("x"))
	if p.sel != at || strings.HasPrefix(p.rows[p.sel], "▾ Opened") {
		t.Fatalf("after x the cursor is on %q (index %d, was %d)", p.rows[p.sel], p.sel, at)
	}
	_ = m
}
