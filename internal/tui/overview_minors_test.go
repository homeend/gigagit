package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// wideTable is an overview whose only anchor sits in a table column far past
// any reading width.
var wideTable = "# Wide\n\n| what | where |\n|---|---|\n| " + strings.Repeat("long cell ", 30) + "| [far](a.txt:3) |\n"

func ctrlW() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlW} }

// visibleText is the overview window as the user sees it, plain.
func visibleText(m Model, fv *fileViewer) string {
	return ansi.Strip(fv.render(m, ""))
}

func TestTabPansToAnAnchorPastAWideTablesEdge(t *testing.T) {
	t.Parallel()
	m, d := shownOverview(t, wideTable)
	fv := layerOf[*fileViewer](m)
	if d.p.mode != modeScroll {
		t.Fatalf("mode = %v, want horizontal scroll by default", d.p.mode)
	}
	if strings.Contains(visibleText(m, fv), "far") {
		t.Fatal("fixture: the anchor is already on screen")
	}
	m = fvKeys(t, m, keyMsg("tab"))
	if d.ov.sel != 0 {
		t.Fatalf("tab: sel = %d, want the far anchor", d.ov.sel)
	}
	if d.p.hscroll == 0 || !strings.Contains(visibleText(m, fv), "far") {
		t.Fatalf("tab did not pan to the anchor: hscroll=%d\n%s", d.p.hscroll, visibleText(m, fv))
	}
}

func TestShiftArrowsPanAnOverview(t *testing.T) {
	t.Parallel()
	m, d := shownOverview(t, wideTable)
	m = fvKeys(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	if d.p.hscroll == 0 {
		t.Fatal("shift+right did not pan the overview")
	}
	fvKeys(t, m, tea.KeyMsg{Type: tea.KeyShiftLeft})
	if d.p.hscroll != 0 {
		t.Fatalf("shift+left: hscroll = %d", d.p.hscroll)
	}
}

func TestCutoffViewLeavesACutAnchorOutOfTabsReach(t *testing.T) {
	t.Parallel()
	m, d := shownOverview(t, wideTable)
	m = fvKeys(t, m, ctrlW())
	if d.p.mode != modeCutoff {
		t.Fatalf("ctrl+w: mode = %v, want cutoff", d.p.mode)
	}
	fv := layerOf[*fileViewer](m)
	if !strings.Contains(visibleText(m, fv), "…") {
		t.Fatalf("a cut row shows no ellipsis:\n%s", visibleText(m, fv))
	}
	fvKeys(t, m, keyMsg("tab"))
	if d.ov.sel != -1 {
		t.Fatalf("tab reached an anchor the user chose to cut off: sel = %d", d.ov.sel)
	}
}

func TestWrapViewBreaksAWideRowAndTabReachesIt(t *testing.T) {
	t.Parallel()
	m, d := shownOverview(t, wideTable)
	m = fvKeys(t, m, ctrlW(), ctrlW())
	if d.p.mode != modeWrap {
		t.Fatalf("ctrl+w twice: mode = %v, want wrap", d.p.mode)
	}
	for i, l := range d.p.lines {
		if lipgloss.Width(l.text) > d.ov.w {
			t.Fatalf("line %d is %d wide, over the %d column", i, lipgloss.Width(l.text), d.ov.w)
		}
	}
	m = fvKeys(t, m, keyMsg("tab"))
	fv := layerOf[*fileViewer](m)
	if d.ov.sel != 0 || !strings.Contains(visibleText(m, fv), "far") {
		t.Fatalf("wrap: sel=%d\n%s", d.ov.sel, visibleText(m, fv))
	}
}

func TestDoubleClickOnAPannedAnchorOpensIt(t *testing.T) {
	t.Parallel()
	m, d := shownOverview(t, wideTable)
	m = fvKeys(t, m, keyMsg("tab"))
	w, _ := m.overlayDims()
	_, margin := readingColumn(w-4, m.readingWidth())
	if len(d.ov.anchors[0].spans) == 0 {
		t.Fatal("the far anchor has no place on screen")
	}
	s := d.ov.anchors[0].spans[0]
	col := lipgloss.Width(string([]rune(d.p.lines[s.line].text)[:s.from])) - d.p.hscroll
	click := tea.MouseMsg{X: 2 + margin + col + 1, Y: 2 + s.line - d.p.sel, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	for range 2 {
		tm, cmd := m.Update(click)
		m = pumpAll(t, tm.(Model), cmd)
	}
	if f := topDoc(m); f == nil || f.path != "a.txt" {
		t.Fatalf("a double click on the panned anchor did not open it: top %v", f)
	}
}

func TestALabelOverALineBreakKeepsTheSpace(t *testing.T) {
	t.Parallel()
	_, d := shownOverview(t, "see [two\nwords](a.txt) here\n")
	if got := anchorText(d.p.lines, d.ov.anchors[0]); got != "two words" {
		t.Fatalf("label = %q, want %q", got, "two words")
	}
}

func TestOpenAnchorChecksTheFileOffTheUIThread(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	rows, _ := m.viewerGeom()
	d.selectAnchor(1, rows)
	nm, cmd := m.openAnchor(d, 1)
	if topDoc(nm) != d || cmd == nil {
		t.Fatalf("openAnchor opened at once (top %v): its stat ran on the UI thread", topDoc(nm))
	}
	nm = pumpAll(t, nm, cmd)
	if f := topDoc(nm); f == nil || f.path != "a.txt" || f.from != d {
		t.Fatalf("after the check: top %+v", f)
	}
}

func TestOpenAnchorCheckLandingAfterTheUserLeftOpensNothing(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	nm, cmd := m.openAnchor(d, 1)
	nm = nm.backgroundDoc(d) // the user stepped aside before the check answered
	nm = pumpAll(t, nm, cmd)
	if f := topDoc(nm); f != nil && f.path == "a.txt" {
		t.Fatal("a late check opened the file over whatever the user went to")
	}
}

func TestDoubleClickOnAWrappedAnchorAfterTheFirstClickScrolls(t *testing.T) {
	t.Parallel()
	filler := strings.Repeat("filler line\n\n", 40)
	label := strings.Repeat("a label long enough to wrap ", 6)
	m, d := shownOverview(t, filler+"lead ["+strings.TrimSpace(label)+"](a.txt) tail\n\n"+filler)
	a := d.ov.anchors[0]
	if len(a.spans) < 2 {
		t.Fatalf("fixture: the label did not wrap: %+v", a.spans)
	}
	s := a.spans[1]
	d.p.sel = s.line // the label's first row is just above the window
	w, _ := m.overlayDims()
	_, margin := readingColumn(w-4, m.readingWidth())
	click := tea.MouseMsg{X: 2 + margin + s.from + 1, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	for range 2 {
		tm, cmd := m.Update(click)
		m = pumpAll(t, tm.(Model), cmd)
	}
	if f := topDoc(m); f == nil || f.path != "a.txt" {
		t.Fatalf("the double click missed after the first click scrolled: top %v sel=%d", f, d.ov.sel)
	}
}

func TestTheOpenFilesCapLeavesRoomBeside20Overviews(t *testing.T) {
	t.Parallel()
	r := &openFilesReg{}
	never := func(*openFile) bool { return false }
	for i := range 20 {
		o := newOpenFile(fileSource{kind: srcWorktree}, "o"+string(rune('a'+i)))
		o.ov = &overview{sel: -1}
		r.touch("/wt", o, never)
	}
	for i := range 30 {
		if ev := r.touch("/wt", newOpenFile(fileSource{kind: srcWorktree}, "file"+strings.Repeat("x", i)), never); ev != nil {
			t.Fatalf("file %d pushed %s out with 20 overviews open", i, ev.path)
		}
	}
}
