package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/theme"
)

// splitDoc is a loaded commit document of n lines "<prefix>1".."<prefix>n" —
// more lines than a pane shows, so scrolling has somewhere to go.
func splitDoc(path, prefix string, n int) *openFile {
	d := &openFile{src: fileSource{kind: srcCommit, rev: "abc"}, path: path, p: &contentPopup{}, tag: path}
	for i := 1; i <= n; i++ {
		l := prefix + strconv.Itoa(i)
		d.p.lines = append(d.p.lines, contentLine{text: l, raw: l, src: true})
	}
	return d
}

// A split over two documents: left/right halves, each a bordered box with
// its own title, one hint line under them.
func testSplit(t *testing.T) (Model, *splitView) {
	t.Helper()
	m := newTestModel(t)
	m.width, m.height = 120, 30
	sv := &splitView{onLeft: true}
	sv.left = &docPane{d: splitDoc("left.md", "l", 60)}
	sv.right = &docPane{d: splitDoc("right.go", "r", 60)}
	m = m.pushLayer(sv)
	return m, sv
}

func TestSplitGeometryIsTwoHalves(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	lw, rw, bh := m.splitGeom()
	if lw != 60 || rw != 60 || bh != 29 {
		t.Fatalf("geom = %d %d %d", lw, rw, bh)
	}
	rows, inner := sv.geomOf(m, sv.left)
	if rows != 26 || inner != 56 {
		t.Fatalf("pane geom = %d×%d, want 26×56 (box 60×29 less border, title, padding)", rows, inner)
	}
	m.width = 121
	lw, rw, _ = m.splitGeom()
	if lw != 60 || rw != 61 {
		t.Fatalf("odd width: %d %d", lw, rw)
	}
}

func TestSplitRendersAtTinyWidth(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	m.width, m.height = 9, 5
	if v := sv.render(m, ""); v == "" {
		t.Fatal("a tiny split must still draw")
	}
}

func TestSplitFocusKeys(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	for _, k := range []string{"tab", "shift+tab", "alt+right", "alt+left"} {
		before := sv.onLeft
		m = press(t, m, k)
		if sv.onLeft == before {
			t.Fatalf("%s did not switch sides", k)
		}
	}
	sv.onLeft = false
	m = press(t, m, "backspace")
	if !sv.onLeft {
		t.Fatal("backspace focuses the left side")
	}
	press(t, m, "backspace")
	if !sv.onLeft {
		t.Fatal("backspace on the left is inert")
	}
}

func TestSplitKeysReachOnlyTheFocusedPane(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	m = press(t, m, "down")
	if sv.left.doc().p.sel != 1 || sv.right.doc().p.sel != 0 {
		t.Fatalf("left %d right %d: only the focused pane scrolls", sv.left.doc().p.sel, sv.right.doc().p.sel)
	}
	m = press(t, m, "tab")
	press(t, m, "down")
	if sv.right.doc().p.sel != 1 {
		t.Fatal("after tab the right pane scrolls")
	}
}

func TestSplitEscPopsAndRunsOnClose(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	closed := false
	sv.onClose = func(m Model) Model { closed = true; m.statusMsg = "closed"; return m }
	m = press(t, m, "esc")
	if m.topLayer() != nil || !closed || m.statusMsg != "closed" {
		t.Fatalf("top %T closed %v msg %q", m.topLayer(), closed, m.statusMsg)
	}
}

func TestSplitEscGoesToATypingSearchFirst(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	m = press(t, m, "/")
	if !sv.left.doc().p.search.typing {
		t.Fatal("/ opens the focused pane's search")
	}
	m = press(t, m, "esc")
	if m.topLayer() != sv || sv.left.doc().p.search.typing {
		t.Fatalf("esc closes the search, not the split: top %T typing %v", m.topLayer(), sv.left.doc().p.search.typing)
	}
	// A live selection owns esc too.
	m = press(t, m, " ")
	if !sv.left.doc().p.lsel.on {
		t.Fatal("space starts a selection")
	}
	m = press(t, m, "esc")
	if m.topLayer() != sv || sv.left.doc().p.lsel.on {
		t.Fatal("esc unmarks the selection, the split stays")
	}
}

func TestSplitSeamsAnswerForTheFocusedPane(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	if d, ok := m.focusedDoc(); !ok || d != sv.left.doc() {
		t.Fatalf("focusedDoc = %v %v", d, ok)
	}
	if p, rows, _, ok := m.activePreview(); !ok || p != sv.left.doc().p || rows != 26 {
		t.Fatalf("activePreview = %v rows %d ok %v", p, rows, ok)
	}
	if topDoc(m) != sv.left.doc() {
		t.Fatal("topDoc is the focused pane's document")
	}
	sv.onLeft = false
	if d, _ := m.focusedDoc(); d != sv.right.doc() {
		t.Fatal("after a switch the right pane answers")
	}
	if !m.docShown(sv.left.doc()) || !m.docShown(sv.right.doc()) {
		t.Fatal("both pane documents are shown")
	}
	if m.previewDoc(sv.left.doc().p) != sv.left.doc() {
		t.Fatal("previewDoc finds a pane's document by its popup")
	}
}

func TestSplitLiveDocFindsEitherPane(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	for _, p := range []pane{sv.left, sv.right} {
		d, rows, inner, ok := m.liveDoc(p.doc().tag)
		if !ok || d != p.doc() || rows != 26 || inner != 56 {
			t.Fatalf("liveDoc(%s) = %v %d %d %v", p.doc().tag, d, rows, inner, ok)
		}
	}
	// A load landing for the unfocused pane fills that pane, not the focused one.
	u, _ := m.Update(fileContentMsg{slotStamp: m.stamp(), tag: "right.go", lines: []contentLine{{text: "new", src: true}}})
	m = u.(Model)
	if sv.right.doc().p.lines[0].text != "new" || sv.left.doc().p.lines[0].text != "l1" {
		t.Fatal("the result landed in the wrong pane")
	}
}

// A document leaves its pane for another frame (a document is in one frame
// at a time): the file pane empties and the split stays; the left document
// leaving takes the split down, the other document kept in the background.
func TestSplitDetachingAPaneDocument(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	m = m.detachDoc(sv.right.doc())
	if !m.hasLayer(sv) || sv.right.doc() != nil {
		t.Fatal("the file pane empties, the split stays")
	}
	m, sv = testSplit(t)
	right := sv.right.doc()
	m = m.detachDoc(sv.left.doc())
	if m.hasLayer(sv) || !right.backgrounded {
		t.Fatal("the left document leaving closes the split, the right one kept in the background")
	}
}

func TestSplitRenderHasBothBoxesAndOneHint(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	plain := ansi.Strip(sv.render(m, ""))
	if !strings.Contains(plain, "left.md") || !strings.Contains(plain, "right.go") {
		t.Fatalf("both titles drawn:\n%s", plain)
	}
	if strings.Count(plain, "[tab] other side") != 1 {
		t.Fatalf("one hint line:\n%s", plain)
	}
	if n := countBoxTops(plain); n != 1 || strings.Count(strings.SplitN(plain, "\n", 2)[0], "╭") != 2 {
		t.Fatalf("two boxes side by side on one top row:\n%s", plain)
	}
}

// One lit border: the focused pane's box is drawn in the focus colour, the
// other's in the dim one. Sets the colour profile and theme (process-global),
// so it does NOT call t.Parallel().
func TestSplitFocusedBoxIsLit(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark)
	m, sv := testSplit(t)
	if sv.left.render(m, 60, 29, true) == sv.left.render(m, 60, 29, false) {
		t.Fatal("the focused box differs from the unfocused one")
	}
}

// docRows measures a document in the frame that shows it: a split's pane,
// else the full-screen viewer.
func TestDocRowsIsTheFrameShowingTheDocument(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	m.height = 40
	want, _ := sv.geomOf(m, sv.right)
	if got := m.docRows(sv.right.doc()); got != want {
		t.Fatalf("split pane rows %d, want %d", got, want)
	}
	vr, _ := m.viewerGeom()
	if got := m.docRows(splitDoc("elsewhere.go", "e", 3)); got != vr {
		t.Fatalf("a document in no split: %d, want the viewer's %d", got, vr)
	}
}
