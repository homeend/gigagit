package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// tourModel is loadedNavModel with a note on a.txt:20 and an overview whose
// anchors are, in order: the file, line 12, lines 5-8, the note, a missing
// file, a gone note.
func tourModel(t *testing.T) (Model, *openFile, *fileNote) {
	t.Helper()
	m := loadedNavModel(t)
	nm, cmd := m.applySteer(noteAddCmd("tour-n", "a.txt", 20, 20, "look here"))
	m = pumpAll(t, nm, cmd)
	_, n := m.findFileNote(awaitNote(t, m, "tour-n").Notes[0].ID)
	if n == nil {
		t.Fatal("the fixture note was not added")
	}
	text := "# Tour\n\n" +
		"- [the file](a.txt)\n" +
		"- [line twelve](a.txt:12)\n" +
		"- [five to eight](a.txt:5-8)\n" +
		"- [the note](note:" + n.id + ")\n" +
		"- [missing](missing.txt)\n" +
		"- [gone note](note:t999999)\n"
	d := newOverviewDoc("Tour", text)
	m = m.registerDoc(d)
	m, cmd = m.bringToFront(d)
	return pumpAll(t, m, cmd), d, n
}

func TestTabStepsThroughAnchorsAndWraps(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = fvKeys(t, m, keyMsg("tab"))
	if d.ov.sel != 0 || d.p.cur != d.ov.anchors[0].spans[0].line {
		t.Fatalf("first tab: sel=%d cur=%d", d.ov.sel, d.p.cur)
	}
	for i := 1; i < len(d.ov.anchors); i++ {
		m = fvKeys(t, m, keyMsg("tab"))
	}
	if d.ov.sel != len(d.ov.anchors)-1 {
		t.Fatalf("sel = %d, want the last", d.ov.sel)
	}
	fvKeys(t, m, keyMsg("tab"))
	if d.ov.sel != 0 {
		t.Fatalf("tab past the last: sel = %d, want 0", d.ov.sel)
	}
}

func TestShiftTabStepsBack(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = fvKeys(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if d.ov.sel != len(d.ov.anchors)-1 {
		t.Fatalf("shift+tab from none: sel = %d, want the last", d.ov.sel)
	}
	fvKeys(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if d.ov.sel != len(d.ov.anchors)-2 {
		t.Fatalf("sel = %d", d.ov.sel)
	}
}

// openNth selects anchor i and presses enter.
func openNth(t *testing.T, m Model, d *openFile, i int) Model {
	t.Helper()
	rows, _ := m.viewerGeom()
	d.selectAnchor(i, rows)
	return fvKeys(t, m, keyMsg("enter"))
}

func TestEnterOpensAPathAnchorOnTopWithItsLine(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1)
	f := topDoc(m)
	if f == nil || f.path != "a.txt" || f.src.kind != srcWorktree {
		t.Fatalf("top = %+v, want a.txt in the viewer", f)
	}
	if f.p.cur != 11 || f.from != d || !f.backgrounded {
		t.Fatalf("cur=%d from=%v backgrounded=%v", f.p.cur, f.from == d, f.backgrounded)
	}
}

func TestEnterOnARangeSelectsTheLines(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 2)
	f := topDoc(m)
	lo, hi, ok := f.p.lsel.bounds(f.p.cur)
	if f.p.cur != 4 || !ok || lo != 4 || hi != 7 || !f.p.lsel.fixed {
		t.Fatalf("cur=%d sel=%v %d..%d fixed=%v, want 5-8 selected", f.p.cur, ok, lo, hi, f.p.lsel.fixed)
	}
}

func TestEnterOnANoteAnchorLandsOnTheNote(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 3)
	f := topDoc(m)
	if f == nil || f.path != "a.txt" || f.p.cur != 19 || f.from != d {
		t.Fatalf("top = %+v", f)
	}
}

func TestEnterOnAMissingFileMarksItAndStays(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 4)
	if topDoc(m) != d || !d.ov.anchors[4].missing || m.statusMsg != "no file missing.txt" {
		t.Fatalf("top=%v missing=%v status=%q", topDoc(m) == d, d.ov.anchors[4].missing, m.statusMsg)
	}
}

func TestEnterOnAGoneNoteMarksIt(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 5)
	if topDoc(m) != d || !d.ov.anchors[5].missing || m.statusMsg != "note t999999 is gone" {
		t.Fatalf("top=%v missing=%v status=%q", topDoc(m) == d, d.ov.anchors[5].missing, m.statusMsg)
	}
}

func TestBackspaceReturnsToTheOverviewAndKeepsTheFileOpen(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1)
	f := topDoc(m)
	m = fvKeys(t, m, keyMsg("backspace"))
	if topDoc(m) != d || d.ov.sel != 1 {
		t.Fatalf("top is %v, sel %d — want the overview on anchor 1", topDoc(m), d.ov.sel)
	}
	if m.openFiles.find(m.currentWorktree, f.key()) != f {
		t.Fatal("the file was closed")
	}
}

func TestBackspaceAfterTheOverviewClosedSaysSo(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1)
	f := topDoc(m)
	m.openFiles.remove(m.currentWorktree, d)
	m = fvKeys(t, m, keyMsg("backspace"))
	if topDoc(m) != f || m.statusMsg != "the overview was closed" {
		t.Fatalf("top=%v status=%q", topDoc(m), m.statusMsg)
	}
}

func TestBackspaceWithoutAnOriginDoesNothing(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	m = fvKeys(t, m, keyMsg("backspace"))
	if topDoc(m) != d {
		t.Fatal("backspace moved away from a file no anchor opened")
	}
}

func TestLatestJumpWins(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1)
	m = fvKeys(t, m, keyMsg("backspace"))
	d2 := newOverviewDoc("Second", "[again](a.txt:3)")
	m = m.registerDoc(d2)
	var cmd tea.Cmd
	m, cmd = m.bringToFront(d2)
	m = pumpAll(t, m, cmd)
	m = openNth(t, m, d2, 0)
	m = fvKeys(t, m, keyMsg("backspace"))
	if topDoc(m) != d2 {
		t.Fatalf("back went to %v, want the second overview", topDoc(m))
	}
}

func TestReferenceAndCopyKeys(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	copied := new(string)
	m.clipWrite = func(_ io.Writer, s string) (string, error) { *copied = s; return "fake", nil }
	m = fvKeys(t, m, key("r"))
	if *copied != "" {
		t.Fatal("r with no anchor selected copied something")
	}
	m = fvKeys(t, m, keyMsg("tab"), keyMsg("tab"), key("r"))
	if want := `gg overview ` + d.id() + ` "Tour" → a.txt:12`; *copied != want {
		t.Fatalf("copied %q, want %q", *copied, want)
	}
	fvKeys(t, m, key("y"))
	if *copied != d.ov.text {
		t.Fatalf("y copied %q", *copied)
	}
}

func TestClickSelectsThenDoubleClickOpens(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	w, _ := m.overlayDims()
	_, margin := readingColumn(w-4, m.readingWidth())
	s := d.ov.anchors[1].spans[0]
	click := tea.MouseMsg{X: 2 + margin + s.from + 1, Y: 2 + s.line - d.p.sel, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	tm, cmd := m.Update(click)
	m = pumpAll(t, tm.(Model), cmd)
	if d.ov.sel != 1 || topDoc(m) != d {
		t.Fatalf("one click: sel=%d top=%v, want anchor 1 selected", d.ov.sel, topDoc(m))
	}
	tm, cmd = m.Update(click)
	m = pumpAll(t, tm.(Model), cmd)
	if f := topDoc(m); f == nil || f.path != "a.txt" {
		t.Fatalf("double click did not open the anchor: top %v", f)
	}
}

func TestOverviewMenuRows(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = fvKeys(t, m, keyMsg("tab"))
	ids := map[string]bool{}
	for _, r := range m.overviewRows() {
		ids[r.id] = true
	}
	for _, want := range []string{"overview-next", "overview-open", "overview-ref"} {
		if !ids[want] {
			t.Errorf("menu lacks %s: %v", want, ids)
		}
	}
	m = openNth(t, m, d, 1)
	ids = map[string]bool{}
	for _, r := range m.overviewRows() {
		ids[r.id] = true
	}
	if !ids["overview-back"] {
		t.Errorf("a file opened from an anchor lacks Back to overview: %v", ids)
	}
	if v := m.View(); !strings.Contains(v, "[bksp] back") {
		t.Errorf("the file's hint lacks [bksp] back")
	}
}

// The full-screen viewer covers the status bar: a message for the user (a
// missing anchor, a copy) sits on its title line until the next key.
func TestViewerShowsTheStatusMessageOnItsHintLine(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 4)
	if v := m.View(); !strings.Contains(v, "no file missing.txt") {
		t.Fatalf("the missing-anchor notice is not on screen:\n%s", v)
	}
	m = fvKeys(t, m, keyMsg("down"))
	if v := m.View(); strings.Contains(v, "no file missing.txt") || !strings.Contains(v, "[tab] next") {
		t.Fatalf("the notice outlived the next key:\n%s", v)
	}
}
