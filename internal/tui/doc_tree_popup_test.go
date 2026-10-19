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
