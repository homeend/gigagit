package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// v on the split's file pane: the mode copies the line's text and leaves.
func TestSplitFilePaneCharSelCopies(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	var got string
	m = captureClip(m, &got)
	m = pressDrain(t, m, "tab")
	d := sv.right.doc()
	want := string(previewCharText(d.p.lines[d.p.cur]))
	m = pressDrain(t, m, "v")
	if !d.p.cs.on {
		t.Fatal("v on the file pane enters the mode")
	}
	m = pressDrain(t, m, "space")
	m = pressDrain(t, m, "end")
	pressDrain(t, m, "enter")
	if got != want || d.p.cs.on {
		t.Fatalf("copied %q (on=%v), want %q", got, d.p.cs.on, want)
	}
}

// v on the overview pane: up/down move the character cursor, never step
// anchors or steer the file pane.
func TestSplitOverviewPaneCharSelCopiesWithoutSteering(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	var got string
	m = captureClip(m, &got)
	ov := sv.left.doc()
	sel, right := ov.ov.sel, sv.right.doc()
	m = pressDrain(t, m, "v")
	if !ov.p.cs.on {
		t.Fatal("v on the overview pane enters the mode")
	}
	m = pressDrain(t, m, "down")
	m = pressDrain(t, m, "up")
	if ov.ov.sel != sel || sv.right.doc() != right {
		t.Fatalf("the mode stepped the anchor (%d→%d) or steered", sel, ov.ov.sel)
	}
	want := string(previewCharText(ov.p.lines[ov.p.cur]))
	m = pressDrain(t, m, "home")
	m = pressDrain(t, m, "space")
	m = pressDrain(t, m, "end")
	pressDrain(t, m, "y")
	if got != want || got == "" {
		t.Fatalf("copied %q, want %q", got, want)
	}
}

// v on the stacked pane copies one file's text.
func TestSplitStackPaneCharSelCopies(t *testing.T) {
	t.Parallel()
	m, sv, _ := stackedSplitModel(t)
	var got string
	m = captureClip(m, &got)
	m = pressDrain(t, m, "tab")
	d := sv.right.doc()
	want := string(previewCharText(d.p.lines[d.p.cur]))
	m = pressDrain(t, m, "v")
	if !d.p.cs.on {
		t.Fatal("v on the stacked pane enters the mode")
	}
	m = pressDrain(t, m, "space")
	m = pressDrain(t, m, "end")
	pressDrain(t, m, "enter")
	if got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
}

// The mode is modal over the split's own keys: tab and N wait, esc backs
// out of the mode and leaves the split open.
func TestSplitCharSelOwnsTheSplitKeys(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m = pressDrain(t, m, "tab")
	d := sv.right.doc()
	m = pressDrain(t, m, "v")
	for _, k := range []string{"tab", "backspace", "alt+left", "N"} {
		m = pressDrain(t, m, k)
		if sv.onLeft || !d.p.cs.on || sv.right.doc() != d {
			t.Fatalf("%s left the mode's pane (onLeft %v on %v)", k, sv.onLeft, d.p.cs.on)
		}
	}
	m = pressDrain(t, m, "esc")
	if d.p.cs.on || m.splitLayer() != sv {
		t.Fatalf("esc: on %v split %v — it backs out of the mode only", d.p.cs.on, m.splitLayer())
	}
}

// The hint line in the mode is the mode's keys, not the split's.
func TestSplitCharSelHint(t *testing.T) {
	t.Parallel()
	m, _ := reviewSplitModel(t)
	m = pressDrain(t, m, "tab")
	m = pressDrain(t, m, "v")
	v := m.View()
	if !strings.Contains(v, "[enter] copy") || strings.Contains(v, "[tab] other side") {
		t.Fatalf("hint line:\n%s", lastLine(v))
	}
}

// A click on the other side moves the focus and leaves the mode; a click on
// the mode's own side is ignored.
func TestSplitClickLeavesTheModeOnTheOtherSide(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	lw, _, _ := m.splitGeom()
	m = pressDrain(t, m, "tab")
	d := sv.right.doc()
	m = pressDrain(t, m, "v")
	cur, cs := d.p.cur, d.p.cs.cur
	m = click(t, m, lw+5, 2)
	if !d.p.cs.on || d.p.cur != cur || d.p.cs.cur != cs {
		t.Fatalf("a click on the mode's side moved it: cur %d→%d cs %+v→%+v", cur, d.p.cur, cs, d.p.cs.cur)
	}
	click(t, m, 5, 5)
	if !sv.onLeft || d.p.cs.on {
		t.Fatalf("onLeft %v on %v: a click on the other side leaves the mode", sv.onLeft, d.p.cs.on)
	}
}

// Raising the split focuses the overview: the file pane's mode goes.
func TestSplitRaiseLeavesTheMode(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m = pressDrain(t, m, "tab")
	d := sv.right.doc()
	m = pressDrain(t, m, "v")
	m, _ = m.openOverviewSplit(sv.left.doc())
	if !sv.onLeft || d.p.cs.on {
		t.Fatalf("onLeft %v on %v", sv.onLeft, d.p.cs.on)
	}
}

// Every way the split closes leaves its panes' modes: a narrow resize hands
// the overview to the full-screen viewer, never in a mode laid for the pane.
func TestSplitCloseLeavesTheMode(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	m = pressDrain(t, m, "v")
	u, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 30})
	_ = u
	if ov.p.cs.on {
		t.Fatal("the overview came back in the mode")
	}
	m2, sv2 := reviewSplitModel(t)
	m2 = pressDrain(t, m2, "tab")
	d := sv2.right.doc()
	m2 = pressDrain(t, m2, "v")
	m2.dropSplits()
	if d.p.cs.on {
		t.Fatal("dropSplits kept the file pane's mode")
	}
}

// A file arriving in the stack mid-mode keeps the selection on its lines;
// the selection stays inside the file it started in.
func TestSplitStackCharSelSurvivesALoadAndStaysInItsFile(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = pressDrain(t, m, "tab")
	m = pressDrain(t, m, "N")
	m = pressDrain(t, m, "alt+down")
	m = pressDrain(t, m, "alt+down") // b.go line 1
	d := sv.right.doc()
	m = pressDrain(t, m, "v")
	m = pressDrain(t, m, "space")
	m = pressDrain(t, m, "j") // b.go line 2
	sp.stream.files[0].loaded = false
	u, _ := m.Update(stackPaneMsg{slotStamp: m.stamp(), tag: d.tag, gen: sp.gen, file: 0, lines: srcLines(40)})
	m = u.(Model)
	m.View()
	if !d.p.cs.on || d.p.cs.anchor.row != sp.stream.streamLine(1, 1) || d.p.cs.cur.row != sp.stream.streamLine(1, 2) {
		t.Fatalf("cs %+v, want b.go lines 1-2 at %d-%d", d.p.cs, sp.stream.streamLine(1, 1), sp.stream.streamLine(1, 2))
	}
	for range 6 {
		m = pressDrain(t, m, "k")
	}
	if sp.stream.fileAt(d.p.cs.cur.row) != 1 {
		t.Fatalf("the cursor left b.go: row %d", d.p.cs.cur.row)
	}
}

// The split's file pane: the pan width is the pane's text column (no
// reading column — the split lays prose at the pane's width).
func TestSplitActivePreviewWidth(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	_, _, w, _ := m.activePreview()
	_, inner := sv.geomOf(m, sv.left)
	if want := max(inner-sv.left.doc().gutterW(), 1); w != want {
		t.Fatalf("width %d, want %d", w, want)
	}
}

// The mode's rows over a big stack cost one pass over the stream, not a
// file lookup per line (a monorepo review: every file landing rebuilds
// them). Measured against the same lines in ONE file — the same pass with
// no lookup to multiply — so the bound holds on a loaded machine.
func TestStackCharRowsAreLinearInTheStream(t *testing.T) {
	t.Parallel()
	rowsOf := func(files, lines, at int) ([]charRow, *fileStream, time.Duration) {
		s := &fileStream{}
		for range files {
			s.files = append(s.files, streamFile{path: "f.go", lines: srcLines(lines), loaded: true})
		}
		p := &contentPopup{lines: s.build()}
		t0 := time.Now()
		rows := previewCharRows(p, s, at)
		return rows, s, time.Since(t0)
	}
	_, _, one := rowsOf(1, 100000, -1) // every row dead: the bare pass
	rows, s, many := rowsOf(1000, 100, 500)
	if live := s.files[500].body; rows[live].dead || !rows[live-1].dead || !rows[s.files[501].body].dead {
		t.Fatal("only file 500's lines are live")
	}
	if many > 10*one+5*time.Millisecond {
		t.Fatalf("1000 files took %v, the same lines in one file %v: not one pass", many, one)
	}
}

// An op handoff or a steer clears the whole layer stack: a split on it is
// retired the way a close retires it — no pane is left in the copy mode.
func TestClearLayersLeavesTheMode(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m = pressDrain(t, m, "tab")
	d := sv.right.doc()
	m = pressDrain(t, m, "v")
	if !d.p.cs.on {
		t.Fatal("v must enter the mode on the file pane")
	}
	m = m.clearLayers() // a popup's op handoff: the files view stays
	if len(m.layers.entries) != 0 {
		t.Fatalf("the stack must be empty: %d", len(m.layers.entries))
	}
	if d.p.cs.on {
		t.Fatal("clearing the stack kept the file pane's mode")
	}
}

// Text replaced under the copy mode (a reload) ends the mode on the next
// key, and that key goes with it: a ↓ meant for the mode's cursor never
// steps the overview's anchor.
func TestCharSelStaleDropSwallowsTheKey(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	m = pressDrain(t, m, "v")
	if !ov.p.cs.on {
		t.Fatal("v must enter the mode on the overview")
	}
	sel := ov.ov.sel
	ov.p.lines = append([]contentLine(nil), ov.p.lines...) // a reload
	m = pressDrain(t, m, "down")
	if ov.p.cs.on {
		t.Fatal("stale text must end the mode")
	}
	if ov.ov.sel != sel {
		t.Fatalf("the dropped mode's key stepped the anchor: %d → %d", sel, ov.ov.sel)
	}
	_ = m
}

// A move that bumps a stacked file's edge says why the cursor stopped, as
// the stacked diff does.
func TestSplitStackCharSelBumpSaysItStaysInTheFile(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = pressDrain(t, m, "tab")
	m = pressDrain(t, m, "N")
	m = pressDrain(t, m, "alt+down")
	m = pressDrain(t, m, "alt+down") // b.go line 1
	d := sv.right.doc()
	m = pressDrain(t, m, "v")
	m.statusMsg = ""
	for range 4 {
		m = pressDrain(t, m, "k")
	}
	if sp.stream.fileAt(d.p.cs.cur.row) != 1 {
		t.Fatalf("the cursor left b.go: row %d", d.p.cs.cur.row)
	}
	if want := i18n.T("▸ the selection stays in this file"); m.statusMsg != want {
		t.Fatalf("status %q, want %q", m.statusMsg, want)
	}
}
