package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Final review C1: X / ctrl+] on the overview pane close the split (as esc
// does) — a split with no overview has nothing to steer, and a stack load
// landing afterwards must not dereference the missing overview.
func TestSplitXOnTheOverviewClosesTheSplit(t *testing.T) {
	t.Parallel()
	for _, k := range []string{"X", "ctrl+]"} {
		m, sv, sp := stackedSplitModel(t)
		tag, gen := sp.d.tag, sp.gen
		m = pressDrain(t, m, k)
		if m.splitLayer() != nil {
			t.Fatalf("%s on the overview leaves the split open", k)
		}
		if !m.filesView.visible()[m.filesView.sel].overviewDoc {
			t.Fatalf("%s: the tree lands on the Overview row", k)
		}
		u, _ := m.Update(stackPaneMsg{slotStamp: m.stamp(), tag: tag, gen: gen, file: 0, lines: srcLines(3)})
		_ = u
		_ = sv
	}
}

// Final review C2: S with no review behind the split never dereferences a
// nil review — it stacks the overview's anchored files (spec
// 2026-10-10-overview-split-all R4).
func TestSplitSWithoutAReviewStacksTheAnchoredFiles(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m.filesReview = nil
	pressDrain(t, m, "S")
	if sp, ok := sv.right.(*stackPane); !ok || len(sp.stream.files) != 2 {
		t.Fatalf("right %T", sv.right)
	}
}

// Final review C2: a resize too narrow for the files view closes the split
// over it too — the review it steers through is gone.
func TestNarrowResizeClosesTheSplit(t *testing.T) {
	t.Parallel()
	m, _ := reviewSplitModel(t)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 30, Height: 30})
	m = u.(Model)
	if m.splitLayer() != nil {
		t.Fatal("the split outlived the files view it was opened from")
	}
}

// Final review I3: a load landing above a marked range keeps the range on
// the same file lines.
func TestStackSelectionSurvivesALoadAbove(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = pressDrain(t, m, "tab")
	m = pressDrain(t, m, "N")
	m = pressDrain(t, m, "alt+down")
	m = pressDrain(t, m, "alt+down") // b.go line 1
	m = pressDrain(t, m, " ")
	m = pressDrain(t, m, "alt+down") // b.go line 2
	m = pressDrain(t, m, " ")
	d := sv.right.doc()
	if !d.p.lsel.on {
		t.Fatal("space, move, space marks a range")
	}
	sp.stream.files[0].loaded = false
	u, _ := m.Update(stackPaneMsg{slotStamp: m.stamp(), tag: d.tag, gen: sp.gen, file: 0, lines: srcLines(40)})
	_ = u
	if !d.p.lsel.on || d.p.lsel.anchor != sp.stream.streamLine(1, 1) || d.p.lsel.end != sp.stream.streamLine(1, 2) {
		t.Fatalf("lsel %+v, want b.go lines 1-2 at %d-%d", d.p.lsel, sp.stream.streamLine(1, 1), sp.stream.streamLine(1, 2))
	}
}

// Final review I4: the stack's . menu band rows walk the stream and move the
// cursor, not only the overview's selection.
func TestStackMenuBandRowsMoveTheCursor(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = pressDrain(t, m, "tab")
	next, ok := rowByID(m.overviewRows(), "anchor-next")
	if !ok {
		t.Fatal("the stack offers the band rows")
	}
	u, cmd := next.run(m)
	drainCmds(t, u.(Model), cmd)
	d := sv.right.doc()
	if d.anchorCur != "b.go:3" || d.p.cur != sp.stream.streamLine(1, 3) {
		t.Fatalf("anchor %q cur %d", d.anchorCur, d.p.cur)
	}
}

// Final review I4: the files view under a split is not front — its tree
// rows (Delete review, Copy review link: they act on the hidden tree's
// cursor) stay off the split's menu, on either side.
func TestSplitMenuHasNoFilesViewRows(t *testing.T) {
	t.Parallel()
	m, _ := reviewSplitModel(t)
	for _, side := range []string{"left", "right"} {
		for _, r := range availableActions(m) {
			if r.id == "delete-review" || r.id == "copy-review-link" || r.id == "view-file" {
				t.Fatalf("%s: files-view row %q leaked onto the split", side, r.id)
			}
		}
		m = pressDrain(t, m, "tab")
	}
}

// Final review I5: Send to background on the file pane empties the pane (the
// split stays); on the overview it closes the split as esc does; the stream
// has no background row.
func TestSplitBackgroundRowPerPane(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m = pressDrain(t, m, "tab")
	r := sv.right.doc()
	row, ok := m.backgroundRow()
	if !ok {
		t.Fatal("the file pane has a background row")
	}
	u, _ := row.run(m)
	m = u.(Model)
	if m.splitLayer() != sv || sv.right.doc() != nil || !r.backgrounded || !m.openFiles.has(m.currentWorktree, r) {
		t.Fatalf("split %v right %v", m.splitLayer() != nil, sv.right.doc())
	}
	m = pressDrain(t, m, "tab") // the overview
	row, _ = m.backgroundRow()
	u, _ = row.run(m)
	m = u.(Model)
	if m.splitLayer() != nil || !m.filesView.visible()[m.filesView.sel].overviewDoc {
		t.Fatal("backgrounding the overview closes the split onto the Overview row")
	}

	m2, _, _ := stackedSplitModel(t)
	m2 = pressDrain(t, m2, "tab")
	if _, ok := m2.backgroundRow(); ok {
		t.Fatal("the stream is no open file: no background row")
	}
}

// Final review I6: a steer names the anchor in the status line.
func TestSplitSteerStatusNamesTheAnchor(t *testing.T) {
	t.Parallel()
	m, _ := reviewSplitModel(t)
	m = pressDrain(t, m, "down")
	if !strings.Contains(m.statusMsg, "anchor 2/3 · B") {
		t.Fatalf("status %q", m.statusMsg)
	}
}

// Final review I6: a file gone at the tip is named and its anchor marked
// missing.
func TestSplitSteerFailureNamesTheFile(t *testing.T) {
	t.Parallel()
	doc := `{"version":1,"summary":"s","overview":"See [it](a.go:1).","files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"x"}]}]}`
	m, id := reviewViewModel(t, doc)
	m, cmd := m.openReview(id, "review")
	m = drainCmds(t, m, cmd)
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	sv := m.splitLayer()
	sv.left.doc().ov.tip = "0000000000000000000000000000000000000000"
	m = pressDrain(t, m, "down")
	if !strings.Contains(m.statusMsg, "a.go") || !sv.left.doc().ov.anchors[0].missing {
		t.Fatalf("status %q missing %v", m.statusMsg, sv.left.doc().ov.anchors[0].missing)
	}
}

// Final review I6: a file the review deleted is "(not at this commit)" in
// the stream, never read.
func TestStackDeletedFileIsNotAtThisCommit(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m.filesView.lines = []contentLine{{path: "gone.go", status: "D"}, {path: "a.go", status: "M"}}
	sp := m.newStackPane(sv).(*stackPane)
	f := sp.stream.files[0]
	if !f.loaded || f.err == nil || f.err.Error() != "(not at this commit)" {
		t.Fatalf("deleted file = %+v", f)
	}
}

// Final review M7 (re-graded Important): any other key abandons a parked
// step — the landing must not yank the cursor away from where the reader went.
func TestStackOtherKeyAbandonsAParkedStep(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = pressDrain(t, m, "tab")
	sp.stream.files[1].loaded, sp.stream.files[1].lines = false, nil
	sp.rebuild(m)
	u, loadCmd := m.Update(keyMsg("n")) // parks on b.go, its load out
	m = u.(Model)
	if sp.park < 0 {
		t.Fatal("n into an unread file parks the landing")
	}
	m = press(t, m, "P") // the reader goes elsewhere
	at := sv.right.doc().p.cur
	m = drainCmds(t, m, loadCmd)
	if sv.right.doc().p.cur != at {
		t.Fatalf("the late landing moved the cursor %d → %d", at, sv.right.doc().p.cur)
	}
}

// Final review M10 (re-graded Important): S is advertised in the file pane's footer.
func TestDocPaneHintAdvertisesS(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	if h := sv.right.hint(m); !strings.Contains(h, "[S] stacked") {
		t.Fatalf("hint %q", h)
	}
}

// Spec §5: deleting the review closes the split with the review view.
func TestDeletingTheReviewClosesTheSplit(t *testing.T) {
	t.Parallel()
	m, _ := reviewSplitModel(t)
	m, _ = m.onStoredDeleted(storedDeletedMsg{id: m.filesReview.id, review: true})
	if m.splitLayer() != nil {
		t.Fatal("the split outlived its review")
	}
}
