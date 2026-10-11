package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func stackedSplitModel(t *testing.T) (Model, *splitView, *stackPane) {
	t.Helper()
	m, sv := reviewSplitModel(t)
	m = pressDrain(t, m, "S")
	sp, ok := sv.right.(*stackPane)
	if !ok {
		t.Fatalf("right = %T after S", sv.right)
	}
	return m, sv, sp
}

func TestStackPaneOpensEveryReviewFileAndLandsTheAnchor(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	if len(sp.stream.files) != 3 || sp.stream.files[0].path != "a.go" {
		t.Fatalf("files = %+v", sp.stream.files)
	}
	for _, f := range sp.stream.files {
		if !f.loaded || f.err != nil || f.folded {
			t.Fatalf("every file of a small stack loads unfolded: %+v", f)
		}
	}
	if sp.inflight != 0 || !m.reviewStacked || !m.promptStore.StackedReviewFiles() {
		t.Fatalf("inflight %d pref %v/%v", sp.inflight, m.reviewStacked, m.promptStore.StackedReviewFiles())
	}
	d := sv.right.doc()
	if d.p.cur != sp.stream.streamLine(0, 3) || d.anchorCur != "a.go:3" {
		t.Fatalf("cur %d anchor %q: S keeps the cursor's file and line", d.p.cur, d.anchorCur)
	}
	if m.openFiles.has(m.currentWorktree, d) {
		t.Fatal("the stream document is not an open file")
	}
	v := ansi.Strip(sv.render(m, ""))
	if !strings.Contains(v, "━━ a.go") || !strings.Contains(v, "◆1") || !strings.Contains(v, "Files: ") || !strings.Contains(v, "func A()") {
		t.Fatalf("render:\n%s", v)
	}
}

func TestStackPaneSteerScrollsTheStream(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	pressDrain(t, m, "down") // b.go:3
	d := sv.right.doc()
	if d.p.cur != sp.stream.streamLine(1, 3) || d.anchorCur != "b.go:3" {
		t.Fatalf("cur %d anchor %q", d.p.cur, d.anchorCur)
	}
	if sv.right != sp {
		t.Fatal("a stacked steer never replaces the pane")
	}
}

func TestStackPaneFoldKeysAndFileStep(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = pressDrain(t, m, "tab")
	m = pressDrain(t, m, "-")
	d := sv.right.doc()
	if !sp.stream.files[0].folded || d.p.cur != sp.stream.files[0].hdr {
		t.Fatalf("- folds the cursor's file, the cursor on its header: folded %v cur %d", sp.stream.files[0].folded, d.p.cur)
	}
	m = pressDrain(t, m, "-")
	if sp.stream.files[0].folded {
		t.Fatal("- again unfolds")
	}
	m = pressDrain(t, m, "_")
	for _, f := range sp.stream.files {
		if !f.folded {
			t.Fatal("_ folds all")
		}
	}
	m = pressDrain(t, m, "_")
	m = pressDrain(t, m, "N")
	if d.p.cur != sp.stream.files[1].hdr {
		t.Fatalf("N steps to the next header: cur %d", d.p.cur)
	}
	pressDrain(t, m, "P")
	if d.p.cur != sp.stream.files[0].hdr {
		t.Fatalf("P steps back: cur %d", d.p.cur)
	}
}

// The walk is in stream order — file, then line: a.go:1-3, a.go:3, b.go:3.
func TestStackPaneBandWalkCrossesFiles(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = pressDrain(t, m, "tab")
	d, ov := sv.right.doc(), sv.left.doc()
	m = pressDrain(t, m, "n")
	// R2: n walks the bands and marks the overview, never moves its selection.
	if d.anchorCur != "b.go:3" || ov.ov.sel != 0 || ov.ov.here() != 1 || d.p.cur != sp.stream.streamLine(1, 3) {
		t.Fatalf("n: anchor %q sel %d here %d cur %d", d.anchorCur, ov.ov.sel, ov.ov.here(), d.p.cur)
	}
	m = pressDrain(t, m, "n")
	if d.anchorCur != "a.go:1-3" || ov.ov.sel != 0 || ov.ov.here() != 4 || !strings.Contains(m.statusMsg, "wrapped") {
		t.Fatalf("n wraps to the first band: %q sel %d here %d %q", d.anchorCur, ov.ov.sel, ov.ov.here(), m.statusMsg)
	}
	pressDrain(t, m, "p")
	if d.anchorCur != "b.go:3" {
		t.Fatalf("p: %q", d.anchorCur)
	}
}

func TestStackPaneBandWalkOpensAFoldedFile(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = pressDrain(t, m, "tab")
	sp.stream.files[1].folded = true
	sp.rebuild(m)
	pressDrain(t, m, "n") // the next band is in b.go, folded: it unfolds and lands
	d := sv.right.doc()
	if sp.stream.files[1].folded || d.anchorCur != "b.go:3" || d.p.cur != sp.stream.streamLine(1, 3) {
		t.Fatalf("folded %v anchor %q cur %d", sp.stream.files[1].folded, d.anchorCur, d.p.cur)
	}
}

func TestStackLoadAboveCursorKeepsPlace(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	d := sv.right.doc()
	// A long tail below the reader, so a top that keeps the screen row is
	// never clamped away by the stream's end.
	u, _ := m.Update(stackPaneMsg{slotStamp: m.stamp(), tag: d.tag, gen: sp.gen, file: 2, lines: srcLines(80)})
	m = u.(Model)
	m = pressDrain(t, m, "down") // b.go:3
	// a.go loads again, longer: the stream grows above the cursor.
	sp.stream.files[0].loaded = false
	sp.rebuild(m)
	screenRow := d.p.cur - d.p.sel
	u, _ = m.Update(stackPaneMsg{slotStamp: m.stamp(), tag: d.tag, gen: sp.gen, file: 0, lines: srcLines(40)})
	m = u.(Model)
	if sp.stream.fileAt(d.p.cur) != 1 || d.p.cur-sp.stream.files[1].body != 2 {
		t.Fatalf("cursor left b.go:3: cur %d", d.p.cur)
	}
	if d.p.cur-d.p.sel != screenRow {
		t.Fatalf("screen row moved: %d → %d", screenRow, d.p.cur-d.p.sel)
	}
	if sp.inflight != 0 {
		t.Fatalf("inflight %d: an answer nobody counted must not drive it negative", sp.inflight)
	}
}

func TestStackStaleLoadIsDropped(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	d := sv.right.doc()
	before := len(d.p.lines)
	m.Update(stackPaneMsg{slotStamp: m.stamp(), tag: d.tag, gen: sp.gen - 1, file: 0, lines: srcLines(40)})
	if len(d.p.lines) != before {
		t.Fatal("an old generation's load must not land")
	}
}

func TestStackPaneSToggleRoundTripsKeepingTheFile(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = pressDrain(t, m, "tab")
	m = pressDrain(t, m, "N") // b.go header
	m = pressDrain(t, m, "alt+down")
	m = pressDrain(t, m, "alt+down") // b.go line 1 (rule, then body)
	m = pressDrain(t, m, "S")
	dp, ok := sv.right.(*docPane)
	if !ok || dp.d == nil || dp.d.path != "b.go" || dp.d.p.cur != 0 || m.reviewStacked {
		t.Fatalf("right = %T %+v stacked %v", sv.right, dp, m.reviewStacked)
	}
	m = pressDrain(t, m, "S")
	sp2, ok := sv.right.(*stackPane)
	if !ok || sp2 == sp || sp2.stream.fileAt(sv.right.doc().p.cur) != 1 || !m.reviewStacked {
		t.Fatalf("back to a new stack on b.go: %T", sv.right)
	}
}

func TestStackOverAHundredFilesOpensFolded(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	var many []contentLine
	for i := 0; i < 101; i++ {
		many = append(many, contentLine{path: "f" + strconv.Itoa(i) + ".go", status: "M"})
	}
	m.filesView.lines = append(append([]contentLine{}, many...), contentLine{path: "a.go", status: "M"})
	sp := m.newStackPane(sv).(*stackPane)
	folded := 0
	for _, f := range sp.stream.files {
		if f.folded {
			folded++
		}
	}
	if folded != 101 || sp.stream.files[len(sp.stream.files)-1].folded {
		t.Fatalf("folded %d of %d; the target stays open", folded, len(sp.stream.files))
	}
}

// An anchor whose file the stream does not list (the agent's overview text
// gained it after the stack was built) is not a file that is gone: the
// steer marks nothing — only a read decides.
func TestStackSteerToAFileTheStreamLacksMarksNothing(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	ov := sv.left.doc()
	var kept []streamFile
	for _, f := range sp.stream.files {
		if f.path != "b.go" {
			kept = append(kept, f)
		}
	}
	sp.stream.files = kept // the stream lacks b.go, which exists at the tip
	sp.rebuild(m)
	m = pressDrain(t, m, "down") // anchor B, on b.go
	a := ov.ov.anchors[ov.ov.sel]
	if a.target.Path != "b.go" {
		t.Fatalf("sel %d on %q, want anchor B", ov.ov.sel, a.target.Path)
	}
	if a.missing {
		t.Fatal("b.go exists: an anchor the stack does not list must not be marked gone")
	}
}
