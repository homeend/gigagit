package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// selNos is the selection's first and last line number on one side.
func selNos(t *testing.T, v *diffView, old bool) (int, int) {
	t.Helper()
	lo, hi, ok := v.lsel.bounds(v.curLine)
	if !ok {
		t.Fatal("no selection after the landing")
	}
	if old {
		return v.lines[lo].Row.LeftNo, v.lines[hi].Row.LeftNo
	}
	return v.lines[lo].Row.RightNo, v.lines[hi].Row.RightNo
}

func landRange(t *testing.T, m Model, id string, line steer.Line) Model {
	t.Helper()
	m, cmd := m.applySteer(steer.Command{ID: id, Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"}, Line: &line, Wait: true})
	m = pumpDiff(t, m, cmd)
	if m.diffLayer() == nil {
		t.Fatal("no diff view after navigate")
	}
	return m
}

func TestRangeLinkLandingMarksItsLines(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m = landRange(t, m, "r-1", steer.Line{Side: "new", No: 16, End: 19})
	v := m.diffLayer()
	if !v.lsel.on || !v.lsel.fixed {
		t.Fatalf("selection = %+v, want a frozen one", v.lsel)
	}
	if a, b := selNos(t, v, false); a != 16 || b != 19 {
		t.Errorf("marked new lines %d-%d, want 16-19", a, b)
	}
	if row, _ := v.cursorRow(); row.RightNo != 16 || v.onOld {
		t.Errorf("cursor on new line %d (old pane %v), want 16 in the new pane", row.RightNo, v.onOld)
	}
	if !strings.Contains(m.diffNotice, "a.txt:16-19") {
		t.Errorf("notice = %q, want the range", m.diffNotice)
	}
	r, ok := steer.AwaitReply(m.steerDir, "r-1", 2*time.Second)
	if !ok || !r.OK || !strings.Contains(r.Detail, "a.txt:16-19") {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestRangeLinkLandingOnTheOldSide(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m = landRange(t, m, "r-2", steer.Line{Side: "old", No: 3, End: 5})
	v := m.diffLayer()
	if !v.onOld {
		t.Fatal("an old-side range lands in the OLD pane")
	}
	if a, b := selNos(t, v, true); a != 3 || b != 5 {
		t.Errorf("marked old lines %d-%d, want 3-5", a, b)
	}
}

// A fold hiding part of the range is opened; a range reaching past the end is
// marked up to the last line.
func TestRangeLinkLandingOpensFoldsAndClamps(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.diffPartial = true
	m = landRange(t, m, "r-3", steer.Line{Side: "new", No: 3, End: 6})
	if a, b := selNos(t, m.diffLayer(), false); a != 3 || b != 6 {
		t.Errorf("folded: marked %d-%d, want 3-6", a, b)
	}

	m = loadedNavModel(t)
	m = landRange(t, m, "r-4", steer.Line{Side: "new", No: 38, End: 99})
	v := m.diffLayer()
	last := v.lastLineNo(false)
	if a, b := selNos(t, v, false); a != 38 || b != last {
		t.Errorf("clamped: marked %d-%d, want 38-%d", a, b, last)
	}
}

func TestSingleLineLandingMarksNothing(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m = landRange(t, m, "r-5", steer.Line{Side: "new", No: 18})
	if m.diffLayer().lsel.on {
		t.Error("a single-line landing must not start a selection")
	}
}

// The round trip: the link L copies from a selection, navigated back, marks
// the same lines on the same side, and L there copies the same link.
func TestRangeLinkRoundTrip(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m = landRange(t, m, "r-6", steer.Line{Side: "new", No: 16, End: 19})
	m.linkRepoName = "gigagit"
	first, ok := m.contextLinkText()
	if !ok || !strings.Contains(first, "/a.txt:16-19~") {
		t.Fatalf("link = %q ok=%v", first, ok)
	}
	m.diffLayer().lsel.clear()
	m = m.popLayer()
	m = landRange(t, m, "r-7", steer.Line{Side: "new", No: 16, End: 19})
	m.linkRepoName = "gigagit"
	if again, _ := m.contextLinkText(); again != first {
		t.Errorf("after the landing L copies %q, want %q", again, first)
	}
}

func TestContentRangeLinkMarksTheViewersLines(t *testing.T) {
	t.Parallel()
	m, _ := viewerModel(t)
	m = fvKeys(t, m, keyType(27)) // esc: close the viewer the helper opened
	c := steer.Command{ID: "fv-r", Cmd: "navigate", File: "main.go",
		Target: &steer.Target{State: "unstaged"}, HintKind: "view", HintID: "content",
		Line: &steer.Line{Side: "new", No: 2, End: 4}, Wait: true}
	nm, cmd := m.applySteer(c)
	m = pumpAll(t, nm, cmd)
	d, ok := m.focusedDoc()
	if !ok {
		t.Fatal("no viewer after the navigate")
	}
	lo, hi, on := d.p.lsel.bounds(d.p.cur)
	if !on || lo != 1 || hi != 3 || d.p.cur != 1 {
		t.Errorf("selection %d-%d on=%v cur=%d, want lines 2-4 with the cursor on 2", lo+1, hi+1, on, d.p.cur+1)
	}
}

// Stacked, the range is marked inside the file the link names — line numbers
// repeat across the stream.
func TestRangeLinkLandingInAStack(t *testing.T) {
	t.Parallel()
	r := sameRowsTUI(6)
	v := stackViewOf(t, r, r)
	m := diffModel()
	m.height, m.width = 24, 120
	m = m.pushLayer(v)
	m, _ = m.landSteer(v, steer.Command{Cmd: "navigate", File: "f1.go",
		Target: &steer.Target{State: "unstaged"}, Line: &steer.Line{Side: "new", No: 2, End: 4}})
	lo, hi, ok := v.lsel.bounds(v.curLine)
	if !ok || v.lines[lo].file != 1 || v.lines[hi].file != 1 || v.lines[lo].Row.RightNo != 2 || v.lines[hi].Row.RightNo != 4 {
		t.Fatalf("selection %d..%d ok=%v, want lines 2-4 of the second file", lo, hi, ok)
	}
}

// A stale range pasted into # opens nothing and says why, translated.
func TestPastedStaleRangeLinkIsRefused(t *testing.T) {
	t.Parallel()
	m := diffModel()
	p := &gotoCommitPopup{input: newTextField("")}
	m = m.pushLayer(p)
	m, _ = m.resolvedGotoLink(p, gotoLinkResolvedMsg{err: &domain.LinkStaleError{Path: "a.go", First: 3, Last: 5}})
	if want := "the link is no longer valid: lines 3-5 of a.go have changed since it was copied"; p.err != want {
		t.Errorf("prompt error = %q, want %q", p.err, want)
	}
	if _, still := m.topLayer().(*gotoCommitPopup); !still {
		t.Error("the prompt must stay open on a refused link")
	}
}

// The range's END under a trailing fold: the fold is opened, the range is not
// cut at the last line that happened to be visible.
func TestRangeLinkLandingReachesUnderATrailingFold(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.diffPartial = true
	m = landRange(t, m, "r-8", steer.Line{Side: "new", No: 20, End: 30})
	if a, b := selNos(t, m.diffLayer(), false); a != 20 || b != 30 {
		t.Errorf("marked %d-%d, want 20-30", a, b)
	}
	if !strings.Contains(m.diffNotice, "a.txt:20-30") {
		t.Errorf("notice = %q", m.diffNotice)
	}
}

// A range that STARTS past the end of the file lands on the last line and
// marks nothing — as the diff does.
func TestContentRangePastTheEndMarksNothing(t *testing.T) {
	t.Parallel()
	m, _ := viewerModel(t)
	m = fvKeys(t, m, keyType(27))
	c := steer.Command{ID: "fv-p", Cmd: "navigate", File: "main.go",
		Target: &steer.Target{State: "unstaged"}, HintKind: "view", HintID: "content",
		Line: &steer.Line{Side: "new", No: 900, End: 905}, Wait: true}
	nm, cmd := m.applySteer(c)
	m = pumpAll(t, nm, cmd)
	d, ok := m.focusedDoc()
	if !ok {
		t.Fatal("no viewer after the navigate")
	}
	if d.p.lsel.on {
		t.Errorf("selection = %+v, want none", d.p.lsel)
	}
	if d.p.cur != len(d.p.lines)-1 {
		t.Errorf("cursor on line %d, want the last (%d)", d.p.cur+1, len(d.p.lines))
	}
}
