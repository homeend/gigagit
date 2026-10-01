package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
)

const tourText = "# Tour\n\nStart at [the file](a.txt), then [line 12](a.txt:12).\n"

// storeOverview files an overview in m's store, as an agent's add does, and
// returns its document (not yet registered or laid out).
func storeOverview(t *testing.T, m Model, title, text string) *openFile {
	t.Helper()
	o, err := m.docs.AddOverview(domain.CheckoutKey(m.currentWorktree), m.currentWorktree, title, text)
	if err != nil {
		t.Fatal(err)
	}
	return newOverviewDocFrom(o)
}

// shownOverview is loadedNavModel with an overview in the full-screen viewer,
// its rows laid out.
func shownOverview(t *testing.T, text string) (Model, *openFile) {
	t.Helper()
	m := loadedNavModel(t)
	d := storeOverview(t, m, "Tour", text)
	m = m.registerDoc(d)
	m, cmd := m.bringToFront(d)
	return pumpAll(t, m, cmd), d
}

func TestOverviewDocIsABackgroundedInMemoryOpenFile(t *testing.T) {
	t.Parallel()
	o, err := agentdocs.New().AddOverview("/r", "/r", "Tour", tourText)
	if err != nil {
		t.Fatal(err)
	}
	d := newOverviewDocFrom(o)
	if d.id() != o.ID {
		t.Fatalf("id = %q, want the store's %q", d.id(), o.ID)
	}
	if d.src.kind != srcOverview || !d.backgrounded || d.onDisk() || d.ov == nil || d.ov.sel != -1 || d.title != "Tour" {
		t.Fatalf("doc = %+v", d)
	}
	if !strings.HasPrefix(d.id(), "f") {
		t.Fatalf("id = %q", d.id())
	}
}

func TestOverviewDocLoadsFromMemory(t *testing.T) {
	t.Parallel()
	m, d := shownOverview(t, tourText)
	if layerOf[*fileViewer](m) == nil || !docLoaded(d) {
		t.Fatalf("overview not shown and loaded: %+v", d.p.lines)
	}
	if len(d.ov.anchors) != 2 || d.ov.anchors[1].spans == nil || d.ov.w == 0 {
		t.Fatalf("anchors = %+v, w = %d", d.ov.anchors, d.ov.w)
	}
	if d.p.lines[0].text != "Tour" {
		t.Fatalf("first row = %q", d.p.lines[0].text)
	}
}

func TestOverviewNeverEvictedByTheCap(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	ov := storeOverview(t, m, "Tour", tourText)
	m = m.registerDoc(ov)
	for i := 0; i < maxOpenFiles+5; i++ {
		m = m.registerDoc(newOpenFile(fileSource{kind: srcWorktree}, fmt.Sprintf("f%d.txt", i)))
	}
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d == ov {
			return
		}
	}
	t.Fatal("the overview was evicted")
}

func TestOverviewRelayoutKeepsTheSelection(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "Paragraph %d has some words to wrap around and [anchor %d](a.txt:%d) inside it.\n\n", i, i, i+1)
	}
	m, d := shownOverview(t, b.String())
	rows, _ := m.viewerGeom()
	d.selectAnchor(20, rows)
	for _, w := range []int{60, 140} {
		m.width = w
		_ = m.renderPreviewBox(d.p, "Tour", w, m.height, true, true)
		a := d.ov.anchors[20]
		if d.ov.sel != 20 || d.p.cur != a.spans[0].line {
			t.Fatalf("width %d: sel=%d cur=%d, want 20 on line %d", w, d.ov.sel, d.p.cur, a.spans[0].line)
		}
		if d.p.cur < d.p.sel || d.p.cur > d.p.lastVisible(rows) {
			t.Fatalf("width %d: the selected anchor is off screen (top %d, cur %d)", w, d.p.sel, d.p.cur)
		}
	}
}

func TestOverviewSwitcherRowAndProto(t *testing.T) {
	t.Parallel()
	m, d := shownOverview(t, tourText)
	row := ansi.Strip(openFileRowText(d, m.docShown(d)))
	if !strings.Contains(row, "Tour") || !strings.Contains(row, "overview · 2 anchors") {
		t.Fatalf("switcher row = %q", row)
	}
	var found bool
	for _, f := range m.openFilesProto() {
		if f.ID == d.id() {
			found = f.Source == "overview" && f.Title == "Tour"
		}
	}
	if !found {
		t.Fatalf("proto = %+v", m.openFilesProto())
	}
}

func TestOverviewViewerTitleAndHint(t *testing.T) {
	t.Parallel()
	m, _ := shownOverview(t, tourText)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Tour") || !strings.Contains(v, "[tab] next") {
		t.Fatalf("view lacks the title or the overview hint:\n%s", v)
	}
}
