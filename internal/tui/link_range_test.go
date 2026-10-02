package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// rangeLinkModel: a 40-row diff of a.txt, every row "l<n>" on both sides;
// row 20 is deleted-only (old line 21), row 21 added-only (new line 21).
func rangeLinkModel(t *testing.T, state model.FileState) Model {
	t.Helper()
	rows := sameRowsTUI(40)
	for i := range rows {
		rows[i].Left, rows[i].Right = rl(i+1), rl(i+1)
	}
	rows[20] = textdiff.Row{Kind: textdiff.Del, Left: "gone", LeftNo: 21}
	rows[21] = textdiff.Row{Kind: textdiff.Add, Right: "added", RightNo: 21}
	for i := 22; i < 40; i++ {
		rows[i].LeftNo, rows[i].RightNo = i, i
		rows[i].Left, rows[i].Right = rl(i), rl(i)
	}
	m := openedDiffModel(12, rows, []int{20})
	m.linkRepoName, m.currentWorktree = "gigagit", "/repo"
	v := m.diffLayer()
	v.title = "a.txt"
	v.noteAddr = model.FileAddress{State: state, Path: "a.txt", Worktree: "/repo"}
	return m
}

func rl(n int) string { return fmt.Sprintf("l%02d", n) }

func selectRows(v *diffView, from, to int) {
	v.lsel = lineSel{on: true, anchor: from, end: to, fixed: true}
	v.curLine = from
}

func TestRangeLinkFromADiffSelection(t *testing.T) {
	t.Parallel()
	m := rangeLinkModel(t, model.StateUnstaged)
	v := m.diffLayer()
	selectRows(v, 4, 6)
	fp := model.BlockFingerprint([]string{"l05", "l06", "l07"})
	wantLink(t, m, "gg://gigagit/a.txt:5-7~"+fp)
	if !v.lsel.on {
		t.Error("building the link must keep the selection")
	}
	if n := m.linkSelectionLines(); n != 3 {
		t.Errorf("linkSelectionLines = %d, want 3", n)
	}
	row, ok := m.contextLinkRow()
	if !ok || !strings.Contains(row.label, "(3)") {
		t.Errorf("menu row = %q ok=%v, want the selected-lines label", row.label, ok)
	}

	// A loose selection (one space) follows the cursor.
	v.lsel = lineSel{on: true, anchor: 4}
	v.curLine = 5
	wantLink(t, m, "gg://gigagit/a.txt:5-6~"+model.BlockFingerprint([]string{"l05", "l06"}))

	// The old side: the same numbers here, addressed as old.
	selectRows(v, 4, 6)
	v.onOld = true
	wantLink(t, m, "gg://gigagit/a.txt:old:5-7~"+fp)

	// One selected line is the single-line link.
	v.onOld = false
	selectRows(v, 4, 4)
	wantLink(t, m, "gg://gigagit/a.txt:5~"+model.LineFingerprint("l05"))
}

// Gap rows at the edges are not lines of the cursor's side: the range starts
// and ends on rows that have a number there.
func TestRangeLinkTrimsGapRows(t *testing.T) {
	t.Parallel()
	m := rangeLinkModel(t, model.StateUnstaged)
	v := m.diffLayer()
	selectRows(v, 20, 23) // del-only row, add row (new 21), new 22, new 23
	wantLink(t, m, "gg://gigagit/a.txt:21-23~"+model.BlockFingerprint([]string{"added", "l22", "l23"}))

	selectRows(v, 20, 20) // only the deleted row, cursor on the new side
	if got, ok := m.contextLinkText(); ok {
		t.Fatalf("a selection with no line on this side linked %q", got)
	}
	u, _ := m.Update(keyMsg("L"))
	if n := u.(Model).diffNotice; !strings.Contains(n, "nothing to link on this side") {
		t.Errorf("notice = %q", n)
	}
}

func TestRangeLinkOnACommitHasNoFingerprint(t *testing.T) {
	t.Parallel()
	m := rangeLinkModel(t, model.StateCommitted)
	v := m.diffLayer()
	v.noteAddr.Commit = cmpShaA
	selectRows(v, 4, 6)
	wantLink(t, m, "gg://gigagit/a.txt@"+cmpShaA+":5-7")
}

func TestRangeLinkInAPairNamesTheOldSide(t *testing.T) {
	t.Parallel()
	m := compareLinkModel(t, mustCommitEndpoint(cmpShaA), mustCommitEndpoint(cmpShaB))
	v := m.diffLayer()
	selectRows(v, 4, 6)
	v.onOld = true
	wantLink(t, m, "gg://gigagit/a.txt@"+cmpShaA+".."+cmpShaB+":old:5-7")
}

func TestStackedRangeLinkStaysInOneFile(t *testing.T) {
	t.Parallel()
	r := sameRowsTUI(6)
	v := stackViewOf(t, r, r)
	for i := range v.stk.files {
		v.stk.files[i].d.cmp = &compareStamp{left: mustCommitEndpoint(cmpShaA), right: mustCommitEndpoint(cmpShaB), path: v.stk.files[i].path}
	}
	line := func(file, no int) int {
		for i, l := range v.lines {
			if l.file == file && l.isBody() && l.Row.RightNo == no {
				return i
			}
		}
		t.Fatalf("no row for file %d line %d", file, no)
		return -1
	}
	m := diffModel()
	m.linkRepoName, m.currentWorktree = "gigagit", "/repo"
	m = m.pushLayer(v)
	selectRows(v, line(1, 2), line(1, 4))
	wantLink(t, m, "gg://gigagit/f1.go@"+cmpShaA+".."+cmpShaB+":2-4")

	selectRows(v, line(1, 2), line(0, 5))
	v.curLine = line(1, 2)
	if got, ok := m.contextLinkText(); ok {
		t.Fatalf("a selection across two files linked %q", got)
	}
	if sel, _ := m.diffLinkSelection(); !strings.Contains(sel.refusal, "one file") {
		t.Errorf("refusal = %q", sel.refusal)
	}
}

// In the file viewer L with lines marked copies the content link to them.
func TestRangeLinkFromTheFileViewer(t *testing.T) {
	t.Parallel()
	m, copied := viewerModel(t)
	d, ok := m.focusedDoc()
	if !ok || len(d.p.lines) < 4 {
		t.Fatalf("no loaded viewer document")
	}
	d.p.lsel = lineSel{on: true, anchor: 1, end: 3, fixed: true}
	lines := strings.Split(viewerGo, "\n")
	want := "/main.go:2-4~" + model.BlockFingerprint(lines[1:4]) + "?view=content"
	row, ok := m.contextFileLinkRow()
	if !ok || !strings.Contains(row.label, "(3)") {
		t.Fatalf("menu row = %q ok=%v", row.label, ok)
	}
	m = fvKeys(t, m, key("L"))
	if !strings.HasSuffix(*copied, want) {
		t.Fatalf("copied %q, want a link ending %q", *copied, want)
	}
	if d, _ := m.focusedDoc(); !d.p.lsel.on {
		t.Error("L must keep the selection")
	}
}

// A frozen selection keeps its link when the cursor walks into another file
// of the stack: the link names the selection's file.
func TestStackedRangeLinkFollowsTheSelectionNotTheCursor(t *testing.T) {
	t.Parallel()
	r := sameRowsTUI(6)
	v := stackViewOf(t, r, r)
	for i := range v.stk.files {
		v.stk.files[i].d.cmp = &compareStamp{left: mustCommitEndpoint(cmpShaA), right: mustCommitEndpoint(cmpShaB), path: v.stk.files[i].path}
	}
	line := func(file, no int) int {
		for i, l := range v.lines {
			if l.file == file && l.isBody() && l.Row.RightNo == no {
				return i
			}
		}
		t.Fatalf("no row for file %d line %d", file, no)
		return -1
	}
	m := diffModel()
	m.linkRepoName, m.currentWorktree = "gigagit", "/repo"
	m = m.pushLayer(v)
	selectRows(v, line(0, 2), line(0, 4))
	v.curLine = line(1, 5) // the cursor moved on; the marks stay in f0.go
	wantLink(t, m, "gg://gigagit/f0.go@"+cmpShaA+".."+cmpShaB+":2-4")
	if v.curLine != line(1, 5) {
		t.Error("building the link must leave the cursor where it was")
	}
}
