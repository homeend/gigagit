package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
)

// tourModel is loadedNavModel with a note on a.txt:20 and an overview whose
// anchors are, in order: the file, line 12, lines 5-8, the note, a missing
// file, a gone note.
func tourModel(t *testing.T) (Model, *openFile, agentdocs.Note) {
	t.Helper()
	m := loadedNavModel(t)
	nm, cmd := m.applySteer(noteAddCmd("tour-n", "a.txt", 20, 20, "look here"))
	m = pumpAll(t, nm, cmd)
	_, np := m.findFileNote(awaitNote(t, m, "tour-n").Notes[0].ID)
	if np == nil {
		t.Fatal("the fixture note was not added")
	}
	n := *np
	text := "# Tour\n\n" +
		"- [the file](a.txt)\n" +
		"- [line twelve](a.txt:12)\n" +
		"- [five to eight](a.txt:5-8)\n" +
		"- [the note](note:" + n.ID + ")\n" +
		"- [missing](missing.txt)\n" +
		"- [gone note](note:t999999)\n"
	d := storeOverview(t, m, "Tour", text)
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

func TestEnterOnARangeLandsWithoutSelecting(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 2)
	f := topDoc(m)
	if f.p.cur != 4 || f.p.lsel.on {
		t.Fatalf("cur=%d lsel=%v, want line 5 and no selection", f.p.cur, f.p.lsel.on)
	}
	bs := f.bands()
	if len(bs) != 2 || f.curBand(bs) != 0 || bs[0].start != 5 || bs[0].end != 8 || bs[1].start != 12 {
		t.Fatalf("bands=%v cur=%d", bs, f.curBand(bs))
	}
}

func TestBandsGoWhenTheOverviewCloses(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1)
	f := topDoc(m)
	m.openFiles.remove(m.currentWorktree, d)
	if f.bands() != nil {
		t.Fatal("a closed overview still bands its file")
	}
}

func TestBandsFollowAnOverviewSet(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1) // a.txt:12 current
	f := topDoc(m)
	if _, err := m.docs.SetOverview(d.id(), "Tour", "# Tour\n\n- [three](a.txt:3)\n- [nine to ten](a.txt:9-10)\n"); err != nil {
		t.Fatal(err)
	}
	m, _ = m.onAgentDocsChanged()
	bs := f.bands()
	if len(bs) != 2 || bs[0].start != 3 || bs[1].start != 9 || bs[1].end != 10 || f.curBand(bs) != -1 {
		t.Fatalf("after set: bands=%v cur=%d", bs, f.curBand(bs))
	}
}

func TestFileOpenedWithoutAnOverviewHasNoBands(t *testing.T) {
	t.Parallel()
	_, d := notedViewer(t)
	if d.bands() != nil {
		t.Fatal("bands without an overview")
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
	d2 := storeOverview(t, m, "Second", "[again](a.txt:3)")
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

// A note anchor lands on the note's first line AND brings its box in, as far
// as the line stays on screen — a note under a long range must not open with
// its box below the window.
func TestNoteAnchorBringsTheBoxIntoView(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.height = 20
	nm, cmd := m.applySteer(noteAddCmd("box-n", "a.txt", 5, 30, "a long range"))
	m = pumpAll(t, nm, cmd)
	id := awaitNote(t, m, "box-n").Notes[0].ID
	d := storeOverview(t, m, "Tour", "[n](note:"+id+")")
	m = m.registerDoc(d)
	m, cmd = m.bringToFront(d)
	m = pumpAll(t, m, cmd)
	m = openNth(t, m, d, 0)
	f := topDoc(m)
	rows, _ := m.viewerGeom()
	if f.p.cur != 4 || f.p.cur < f.p.sel || f.p.cur > f.p.lastVisible(rows) {
		t.Fatalf("cur=%d top=%d, want line 5 on screen", f.p.cur, f.p.sel)
	}
	if f.p.sel != 4 {
		t.Fatalf("top=%d, want the note's first line at the top so its box comes as far in as it can", f.p.sel)
	}
}

func TestBackspaceForgetsAClosedOverview(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1)
	f := topDoc(m)
	m.openFiles.remove(m.currentWorktree, d)
	m = fvKeys(t, m, keyMsg("backspace"))
	if f.from != nil || strings.Contains(m.View(), "[bksp] back") {
		t.Fatal("the dead way back is still offered")
	}
}

func TestRangeLandingStillShowsTheWayBack(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 2)
	m.statusMsg = ""
	if v := m.View(); !strings.Contains(v, "[bksp] back") {
		t.Fatalf("a landed range hides the way back:\n%s", v)
	}
}

func TestViewerTitleSkipsTheRunningAndStickyMessages(t *testing.T) {
	t.Parallel()
	m, _, _ := tourModel(t)
	m.statusMsg, m.running = "working on it", true
	if strings.Contains(m.View(), "working on it") {
		t.Fatal("an op's working message sits in the viewer title")
	}
	m.running = false
	m.stickyMsg = "working on it"
	if strings.Contains(m.View(), "working on it") {
		t.Fatal("a sticky message sits in the viewer title")
	}
}

func TestNStepsToTheNextBandAndWraps(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 2) // a.txt:5-8 current; bands 5-8, 12
	f := topDoc(m)
	m = fvKeys(t, m, keyMsg("n"))
	if f.p.cur != 11 || f.anchorCur != "a.txt:12" || d.ov.sel != 1 {
		t.Fatalf("n: cur=%d anchorCur=%q sel=%d", f.p.cur, f.anchorCur, d.ov.sel)
	}
	if !strings.Contains(m.statusMsg, "anchor 2/2 in this file · line twelve") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	m = fvKeys(t, m, keyMsg("n"))
	if f.p.cur != 4 || !strings.HasSuffix(m.statusMsg, "· wrapped") {
		t.Fatalf("wrap: cur=%d status=%q", f.p.cur, m.statusMsg)
	}
}

func TestPStepsBackAndBackspaceReturnsOnIt(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1) // a.txt:12
	m = fvKeys(t, m, keyMsg("p"))
	if f := topDoc(m); f.p.cur != 4 || f.anchorCur != "a.txt:5-8" {
		t.Fatalf("p: cur=%d current anchor %q", f.p.cur, f.anchorCur)
	}
	m = fvKeys(t, m, keyMsg("backspace"))
	if topDoc(m) != d || d.ov.sel != 2 {
		t.Fatalf("back on sel %d, want 2 (the anchor stepped to)", d.ov.sel)
	}
}

func TestNAndPAreInertWithoutBands(t *testing.T) {
	t.Parallel()
	m, f := notedViewer(t)
	cur := f.p.cur
	m = fvKeys(t, m, keyMsg("n"))
	fvKeys(t, m, keyMsg("p"))
	if f.p.cur != cur {
		t.Fatal("n / p moved a file with no bands")
	}
}

func TestNoteAnchorOpenShowsBandsNoneCurrent(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 3) // the note on a.txt:20
	f := topDoc(m)
	bs := f.bands()
	if len(bs) != 2 || f.curBand(bs) != -1 {
		t.Fatalf("bands=%v cur=%d", bs, f.curBand(bs))
	}
	fvKeys(t, m, keyMsg("p")) // the cursor on 20: the last band above it is 12
	if f.p.cur != 11 {
		t.Fatalf("p from the note: cur=%d want 11", f.p.cur)
	}
}

func TestBandMenuRowsAndHint(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1)
	ids := map[string]bool{}
	for _, r := range m.overviewRows() {
		ids[r.id] = true
	}
	if !ids["anchor-next"] || !ids["anchor-prev"] || !ids["overview-back"] {
		t.Fatalf("rows = %v", ids)
	}
	m.statusMsg = ""
	if !strings.Contains(m.View(), "[n/p] anchors") {
		t.Fatal("hint lacks [n/p] anchors")
	}
}
