package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Spec §6 (D3): with the diff showing, every link stepped to opens as a
// diff until the split closes, which forgets the choice.
func TestDStaysWhileSteppingAndIsForgottenOnClose(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	a := sv.right.doc()
	m = pressDrain(t, m, "D")
	m = pressDrain(t, m, "N") // the overview's next anchor: b.txt
	d := sv.right.doc()
	if d.path != "b.txt" || d.diff == nil || d.diff.notice != "" || d.diff.rows[d.diff.p.cur].nu != 2 {
		t.Fatalf("stepped to %s diff %+v", d.path, d.diff)
	}
	if a.diff != nil {
		t.Fatal("the file stepped away from shows as the file again")
	}
	ov := sv.left.doc()
	m = pressDrain(t, m, "esc")
	if ov.ov.diff || d.diff != nil {
		t.Fatal("closing the split forgets the choice and drops the diff")
	}
	_ = m
}

// The final-review lesson of plan 1: a D pressed while a step's file loads
// is read when the load lands — the diff shows.
func TestADPressedWhileTheStepLoadsWins(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	u, cmd := m.Update(keyMsg("N")) // the step's load is out, not landed
	m = u.(Model)
	if d := sv.right.doc(); d.path != "b.txt" || !d.loading {
		t.Fatalf("right %s loading %v: the step must still be loading", d.path, d.loading)
	}
	m = pressDrain(t, m, "D") // pressed while loading
	m = drainCmds(t, m, cmd)  // the load lands, then the diff is read
	if d := sv.right.doc(); d.diff == nil {
		t.Fatalf("right %s: the D pressed during the load must win", d.path)
	}
}

func TestFFoldsButKeepsTheBandedAndRemarkedRows(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	m = pressDrain(t, m, "f")
	d := sv.right.doc()
	if !d.diff.partial || !sv.left.doc().ov.diffPartial {
		t.Fatal("f is the overview's choice too")
	}
	if rowOfNew(d.diff.rows, 3) < 0 || rowOfOld(d.diff.rows, 5) < 0 {
		t.Fatal("the banded line and the remarked deleted line are never folded")
	}
	m = pressDrain(t, m, "f")
	if d.diff.partial || len(d.diff.rows) != len(d.diff.all) {
		t.Fatal("f again: the whole file")
	}
}

func TestEnterOpensAFold(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	d := sv.right.doc()
	d.diff.partial = true
	d.refoldDiff(m.docRows(d))
	fold := -1
	for i, r := range d.diff.rows {
		if r.kind == diffFold {
			fold = i
		}
	}
	if fold < 0 {
		t.Fatal("the fixture's l10..l18 fold")
	}
	n := len(d.diff.rows)
	d.diff.p.cur = fold
	m = pressDrain(t, m, "enter")
	if len(d.diff.rows) <= n || d.diff.rows[d.diff.p.cur].kind == diffFold {
		t.Fatalf("rows %d → %d, cursor on %+v", n, len(d.diff.rows), d.diff.rows[d.diff.p.cur])
	}
	_ = m
}

func TestNoDOnceTheOverviewIsGone(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	d := sv.right.doc()
	sv.left.doc().ov.closed = true
	m = pressDrain(t, m, "D")
	if d.diff != nil || sv.left.doc().ov.diff {
		t.Fatal("D does nothing for a gone overview")
	}
}

func TestDFromTheOverviewSideFlipsTheFile(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "tab")
	if !sv.onLeft {
		t.Fatal("tab: the overview side")
	}
	m = pressDrain(t, m, "D")
	if sv.right.doc().diff == nil || !sv.onLeft {
		t.Fatal("D on the overview flips the file, the focus stays")
	}
}

func TestTitleSwitchClickFlips(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	if !strings.Contains(ansi.Strip(m.View()), "file | diff") {
		t.Fatal("the switch shows in the title")
	}
	lw, rw, bh := m.splitGeom()
	_, inner := paneGeom(rw, bh)
	click := func(x int) {
		u, cmd := m.Update(tea.MouseMsg{X: x, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		m = drainCmds(t, u.(Model), cmd)
	}
	diffX := lw + 2 + inner - 2 // the last columns of the title: "diff"
	click(diffX)
	if sv.right.doc().diff == nil {
		t.Fatal("a click on diff flips")
	}
	click(diffX)
	if sv.right.doc().diff == nil {
		t.Fatal("a click on the half in force does nothing")
	}
	click(lw + 2 + inner - len("file | diff")) // "file"
	if sv.right.doc().diff != nil {
		t.Fatal("a click on file flips back")
	}
}

func TestNarrowResizeKeepsTheChoice(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	ov := sv.left.doc()
	u, cmd := m.Update(tea.WindowSizeMsg{Width: 50, Height: 40})
	m = drainCmds(t, u.(Model), cmd)
	if !ov.ov.diff {
		t.Fatal("a resize between split and viewer keeps the choice")
	}
	m = pressDrain(t, m, "enter") // the overview viewer opens its selected anchor
	d := topDoc(m)
	if d == nil || d.diff == nil {
		t.Fatalf("top %T: the narrow viewer opens the link as a diff", m.topLayer())
	}
	m = pressDrain(t, m, "D")
	if d.diff != nil || ov.ov.diff {
		t.Fatal("D in the narrow viewer flips back")
	}
}

// D4: an agent's overview diffs HEAD against the file on disk; its notes
// show under their rows.
func TestAgentOverviewDiffIsHeadToTheWorkingTree(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 160)
	sv := m.splitLayer()
	m = pressDrain(t, m, "enter")
	m = pressDrain(t, m, "D")
	d := sv.right.doc()
	if d.diff == nil || d.diff.notice != "" || d.frameTitle() != "Diff a.txt (HEAD → working tree)" {
		t.Fatalf("diff %+v title %q", d.diff, d.frameTitle())
	}
	if !strings.Contains(ansi.Strip(m.View()), "+ EDITED") {
		t.Fatal("the edited line shows as added")
	}
	m = pressDrain(t, m, "N") // the note anchor: b.txt, new on disk
	b := sv.right.doc()
	if b.path != "b.txt" || b.diff == nil || len(b.rowNotes()) != 1 || b.rowNotes()[0].Start != 3 {
		t.Fatalf("right %s diff %+v notes %+v", b.path, b.diff, b.rowNotes())
	}
}

// D4: a file the working tree leaves as HEAD has it says so.
func TestAgentOverviewUnchangedFileSaysSo(t *testing.T) {
	t.Parallel()
	m := agentOverviewModelOf(t, 160, " And [readme](README.md:1).")
	sv := m.splitLayer()
	m = pressDrain(t, m, "enter")
	m = pressDrain(t, m, "D")
	m = pressDrain(t, m, "N")
	m = pressDrain(t, m, "N")
	d := sv.right.doc()
	if d.path != "README.md" || d.diff == nil || d.diff.notice != "no changes in the working tree — D shows the file" {
		t.Fatalf("right %s diff %+v", d.path, d.diff)
	}
	if !strings.Contains(ansi.Strip(m.View()), "no changes in the working tree — D shows the file") {
		t.Fatal("the notice shows")
	}
}

// Spec §4: an old-side remark sits under its deleted row; d / r say
// read-only, enter opens it, } / { walk the boxes by row.
func TestOldSideRemarkIsReachableAndReadOnly(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	d := sv.right.doc()
	d.diff.p.cur = rowOfOld(d.diff.rows, 5)
	m = pressDrain(t, m, "d")
	if !strings.Contains(m.statusMsg, "read-only") {
		t.Fatalf("d on the deleted line's remark: %q", m.statusMsg)
	}
	m = pressDrain(t, m, "{")
	if r := d.diff.rows[d.diff.p.cur]; r.kind != diffAdd || r.nu != 3 {
		t.Fatalf("{ lands on the remark above (L3), got %+v", r)
	}
	m = pressDrain(t, m, "}")
	if r := d.diff.rows[d.diff.p.cur]; r.kind != diffDel || r.old != 5 {
		t.Fatalf("} lands on the deleted line's remark, got %+v", r)
	}
	m = pressDrain(t, m, "enter")
	if cp, ok := m.topLayer().(*contentPopup); !ok || !strings.Contains(cp.title, "line 5") {
		t.Fatalf("enter opens the whole remark: top %T", m.topLayer())
	}
}

// Spec §5: a marking that covers only deleted rows has no link.
func TestMarkingDeletedRowsOnlyRefusesTheLink(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	d := sv.right.doc()
	d.diff.p.cur = rowOfOld(d.diff.rows, 5)
	m = pressDrain(t, m, " ")
	m = pressDrain(t, m, " ")
	m = pressDrain(t, m, "L")
	if m.statusMsg != "a deleted line has no link in the reviewed version" {
		t.Fatalf("L = %q", m.statusMsg)
	}
}

// A marking over a changed pair links the new line it covers.
func TestMarkingAChangedPairLinksItsNewLine(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	d := sv.right.doc()
	d.diff.p.cur = rowOfOld(d.diff.rows, 3) // −l3, then +L3
	m = pressDrain(t, m, " ")
	m = pressDrain(t, m, "alt+down")
	line, last, ok := d.diffMarkedLines()
	if !ok || line != 3 || last != 3 {
		t.Fatalf("marked lines %d..%d ok %v, want 3..3", line, last, ok)
	}
	_ = m
}

// Spec §5: on a deleted row the copied line is the deleted text, named by
// its old number.
func TestCopyLineOnADeletedRowNamesItsOldNumber(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	d := sv.right.doc()
	d.diff.p.cur = rowOfOld(d.diff.rows, 5)
	rows := m.previewCopyLineRows()
	if len(rows) == 0 || rows[0].id != "copy-line" {
		t.Fatalf("rows %+v", rows)
	}
	var got string
	m = captureClip(m, &got)
	u, cmd := rows[0].run(m)
	m = drainCmds(t, u.(Model), cmd)
	if got != "l5" || !strings.Contains(m.statusMsg, "Copied line 5") {
		t.Fatalf("copied %q (%q), want the deleted line's text as line 5", got, m.statusMsg)
	}
}
