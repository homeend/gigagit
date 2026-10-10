package tui

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A review of stackRepoModel's tip (a.go, b.go, c.go each gain a func on
// line 3) with an overview of three resolvable anchors across two files, one
// plain (zzz.go) and one note: anchor — opened as the split.
func reviewSplitModel(t *testing.T) (Model, *splitView) {
	t.Helper()
	return reviewSplitModelOf(t, "Start at [A](a.go:3), then [B](b.go:3), [gone](zzz.go:1), [a note](note:t1), last [A again](a.go:1-3).")
}

// reviewSplitModelOf is reviewSplitModel over a review whose overview is
// text.
func reviewSplitModelOf(t *testing.T, text string) (Model, *splitView) {
	t.Helper()
	ov, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	doc := `{"version":1,"summary":"## Summary\nfine","overview":` + string(ov) + `,"files":[{"path":"a.go","annotations":[{"newRange":[3,3],"summary":"A"}]},{"path":"b.go","annotations":[{"newRange":[3,3],"summary":"B"}]}]}`
	m, id := reviewViewModel(t, doc)
	m, cmd := m.openReview(id, "review")
	m = drainCmds(t, m, cmd)
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	sv := m.splitLayer()
	if sv == nil || m.topLayer() != sv {
		t.Fatalf("top = %T, want the split", m.topLayer())
	}
	return m, sv
}

// pressDrain presses k and runs every command it starts to a fixed point.
func pressDrain(t *testing.T, m Model, k string) Model {
	t.Helper()
	u, cmd := m.Update(keyMsg(k))
	return drainCmds(t, u.(Model), cmd)
}

func TestOverviewSplitOpensWithTheFirstAnchorShown(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	if ov == nil || ov.ov == nil || ov.src.kind != srcReviewOverview || ov.ov.sel != 0 || !sv.onLeft {
		t.Fatalf("left = %+v onLeft %v", ov, sv.onLeft)
	}
	r := sv.right.doc()
	if r == nil || r.path != "a.go" || r.src.kind != srcCommit || r.src.rev != ov.ov.tip || !docLoaded(r) || r.p.cur != 2 {
		t.Fatalf("right = %+v", r)
	}
	if r.from != ov || r.anchorCur != "a.go:3" {
		t.Fatalf("from %v anchorCur %q: the bands need the way back", r.from, r.anchorCur)
	}
	if !m.openFiles.has(m.currentWorktree, r) || !m.openFiles.has(m.currentWorktree, ov) {
		t.Fatal("both documents are registered open files")
	}
}

func TestOverviewSplitDownStepsAnchorsAndSteers(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	m = pressDrain(t, m, "down")
	if ov.ov.sel != 1 || sv.right.doc().path != "b.go" || sv.right.doc().p.cur != 2 || !sv.onLeft {
		t.Fatalf("sel %d right %s cur %d onLeft %v", ov.ov.sel, sv.right.doc().path, sv.right.doc().p.cur, sv.onLeft)
	}
	m = pressDrain(t, m, "down") // zzz.go and note:t1 are plain: skipped
	if ov.ov.sel != 4 || sv.right.doc().path != "a.go" || sv.right.doc().p.cur != 0 || sv.right.doc().anchorCur != "a.go:1-3" {
		t.Fatalf("sel %d right %s cur %d anchor %q", ov.ov.sel, sv.right.doc().path, sv.right.doc().p.cur, sv.right.doc().anchorCur)
	}
	if sv.right.doc().p.lsel.on {
		t.Fatal("a range anchor is a band, not a selection")
	}
	m = pressDrain(t, m, "down") // wraps
	if ov.ov.sel != 0 {
		t.Fatalf("wrap: sel %d", ov.ov.sel)
	}
	m = pressDrain(t, m, "up")
	if ov.ov.sel != 4 {
		t.Fatalf("up wraps back: sel %d", ov.ov.sel)
	}
	// The same file again: the document is reused, not reopened.
	before := sv.right.doc()
	m = pressDrain(t, m, "up") // b.go
	pressDrain(t, m, "down")
	if sv.right.doc() != before {
		t.Fatal("steering back to a file reuses its open document")
	}
}

func TestOverviewSplitEnterFocusesRightAndBackspaceReturns(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m = pressDrain(t, m, "enter")
	if sv.onLeft {
		t.Fatal("enter focuses the right pane")
	}
	// a.go has two bands, 1-3 (sorted first) and 3; on 3, n wraps to 1-3:
	// the overview marks that anchor and keeps its selection (R2).
	m = pressDrain(t, m, "n")
	if sv.left.doc().ov.sel != 0 || sv.left.doc().ov.here() != 4 || sv.right.doc().anchorCur != "a.go:1-3" {
		t.Fatalf("sel %d here %d anchor %q", sv.left.doc().ov.sel, sv.left.doc().ov.here(), sv.right.doc().anchorCur)
	}
	m = pressDrain(t, m, "backspace")
	if !sv.onLeft || m.topLayer() != sv {
		t.Fatal("backspace focuses the overview, the split stays")
	}
}

func TestOverviewSplitPageKeysKeepTheSelection(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	for _, k := range []string{"pgdown", "pgup", "end", "home"} {
		m = pressDrain(t, m, k)
		if ov.ov.sel != 0 {
			t.Fatalf("%s changed the selection to %d", k, ov.ov.sel)
		}
	}
}

func TestOverviewSplitEscLandsOnTheOverviewRow(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov, r := sv.left.doc(), sv.right.doc()
	m = pressDrain(t, m, "tab")
	m = pressDrain(t, m, "down")
	m = pressDrain(t, m, "esc")
	if m.topLayer() != nil || m.filesReview == nil {
		t.Fatalf("top %T review %v: esc returns to the review tree", m.topLayer(), m.filesReview != nil)
	}
	if !m.filesView.visible()[m.filesView.sel].overviewDoc {
		t.Fatalf("cursor on row %d, want the ≡ Overview row", m.filesView.sel)
	}
	if !ov.backgrounded || !r.backgrounded || !m.openFiles.has(m.currentWorktree, ov) || !m.openFiles.has(m.currentWorktree, r) {
		t.Fatal("both documents stay open in the background")
	}
	// Enter again finds the same overview document, selection kept.
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	if sv2 := m.splitLayer(); sv2 == nil || sv2.left.doc() != ov {
		t.Fatal("reopening reuses the overview document")
	}
}

func TestOverviewSplitNarrowTerminalOpensTheViewer(t *testing.T) {
	t.Parallel()
	m, _ := reviewWithOverview(t)
	m.width = 59 // the review tree refuses every row this narrow; a direct caller gets the viewer
	m, cmd := m.openReviewOverviewSplit()
	m = drainCmds(t, m, cmd)
	if _, ok := m.topLayer().(*fileViewer); !ok || m.splitLayer() != nil {
		t.Fatalf("top %T: below %d columns today's viewer opens", m.topLayer(), splitMinWidth)
	}
}

func TestSplitSteerToMissingFileShowsPlaceholder(t *testing.T) {
	t.Parallel()
	doc := `{"version":1,"summary":"s","overview":"See [it](a.go:1).","files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"x"}]}]}`
	m, id := reviewViewModel(t, doc)
	m, cmd := m.openReview(id, "review")
	m = drainCmds(t, m, cmd)
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	sv := m.splitLayer()
	// The file is gone at the commit the anchors open at.
	sv.left.doc().ov.tip = "0000000000000000000000000000000000000000"
	m = pressDrain(t, m, "down")
	r := sv.right.doc()
	if r == nil || docLoaded(r) || r.src.rev != sv.left.doc().ov.tip {
		t.Fatalf("right %+v: the steer opens the file at the overview's tip", r)
	}
	if v := ansi.Strip(sv.render(m, "")); !strings.Contains(v, "load failed") {
		t.Fatalf("a file gone at the tip shows the load placeholder:\n%s", v)
	}
}

func TestOverviewSplitRenderShowsBothSides(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	v := ansi.Strip(sv.render(m, ""))
	for _, want := range []string{"Overview: ", "Start at", "View a.go @", "func A()", "[↑↓] anchor"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
}

// The . menu in a split: the overview's rows walk and open anchors by the
// split's keys and steer the file pane; the file pane offers no "Back to
// overview" (backspace is the split's, the overview is already on screen).
func TestOverviewSplitMenuRows(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	rows := m.overviewRows()
	next, ok := rowByID(rows, "overview-next")
	if !ok || next.key != "↓" {
		t.Fatalf("next row %+v ok %v", next, ok)
	}
	if prev, ok := rowByID(rows, "overview-prev"); !ok || prev.key != "↑" {
		t.Fatalf("prev row %+v ok %v", prev, ok)
	}
	if _, ok := rowByID(rows, "overview-ref"); ok {
		t.Fatal("a stored overview has no reference row")
	}
	u, cmd := next.run(m)
	m = drainCmds(t, u.(Model), cmd)
	if sv.left.doc().ov.sel != 1 || sv.right.doc().path != "b.go" {
		t.Fatalf("the row steers: sel %d right %s", sv.left.doc().ov.sel, sv.right.doc().path)
	}
	open, ok := rowByID(m.overviewRows(), "overview-open")
	if !ok {
		t.Fatal("open row")
	}
	u, cmd = open.run(m)
	m = drainCmds(t, u.(Model), cmd)
	if sv.onLeft {
		t.Fatal("Open anchor focuses the file pane")
	}
	if _, ok := rowByID(m.overviewRows(), "overview-back"); ok {
		t.Fatal("no Back to overview row in a split")
	}
	if _, ok := rowByID(m.overviewRows(), "anchor-next"); !ok {
		t.Fatal("the file pane keeps its band rows")
	}
}

// N / P on the single file pane step the overview's anchors in its own
// order — ↑ / ↓ on the overview — crossing files, the focus kept right.
func TestOverviewSplitNPOnTheFilePaneStepOverviewAnchors(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	m = pressDrain(t, m, "enter")
	m = pressDrain(t, m, "N")
	if ov.ov.sel != 1 || sv.right.doc().path != "b.go" || sv.right.doc().p.cur != 2 || sv.onLeft {
		t.Fatalf("N: sel %d right %s cur %d onLeft %v", ov.ov.sel, sv.right.doc().path, sv.right.doc().p.cur, sv.onLeft)
	}
	if !strings.Contains(m.statusMsg, "anchor 2/") {
		t.Fatalf("status %q: the step names the anchor", m.statusMsg)
	}
	m = pressDrain(t, m, "N") // zzz.go and note:t1 are plain: skipped
	if ov.ov.sel != 4 || sv.right.doc().path != "a.go" || sv.right.doc().anchorCur != "a.go:1-3" || sv.onLeft {
		t.Fatalf("N again: sel %d right %s anchor %q", ov.ov.sel, sv.right.doc().path, sv.right.doc().anchorCur)
	}
	m = pressDrain(t, m, "P")
	if ov.ov.sel != 1 || sv.right.doc().path != "b.go" || sv.onLeft {
		t.Fatalf("P: sel %d right %s onLeft %v", ov.ov.sel, sv.right.doc().path, sv.onLeft)
	}
	if h := sv.right.hint(m); !strings.Contains(h, "[N/P] overview") {
		t.Fatalf("hint %q: N/P is advertised", h)
	}
}

// longTail is n short paragraphs: text taller than a split pane.
func longTail(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("\n\npara " + strconv.Itoa(i))
	}
	return b.String()
}

// ↓ on the last anchor with text still below it pages down; it wraps to the
// first anchor only once the end is on screen.
func TestOverviewSplitDownPagesPastTheLastAnchor(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModelOf(t, "Start at [A](a.go:3), then [B](b.go:3)."+longTail(80)+"\n\nTHE END")
	ov := sv.left.doc()
	m = pressDrain(t, m, "down")
	if ov.ov.sel != 1 {
		t.Fatalf("sel %d, want the last anchor", ov.ov.sel)
	}
	rows, _ := sv.geomOf(m, sv.left)
	pages := 0
	for top := ov.p.clampTop(ov.p.sel, rows); ov.p.clampTop(top+1, rows) > top; top = ov.p.clampTop(ov.p.sel, rows) {
		m = pressDrain(t, m, "down")
		if ov.ov.sel != 1 {
			t.Fatalf("page %d: ↓ wrapped to anchor %d with text below (top %d)", pages, ov.ov.sel, top)
		}
		if now := ov.p.clampTop(ov.p.sel, rows); now <= top {
			t.Fatalf("page %d: ↓ did not scroll (top %d → %d)", pages, top, now)
		}
		pages++
	}
	if pages < 2 {
		t.Fatalf("the tail must take pages: %d", pages)
	}
	if !strings.Contains(ansi.Strip(m.View()), "THE END") {
		t.Fatal("the end is not on screen")
	}
	m = pressDrain(t, m, "down")
	if ov.ov.sel != 0 || ov.p.clampTop(ov.p.sel, rows) != 0 {
		t.Fatalf("at the end ↓ wraps to the first anchor: sel %d top %d", ov.ov.sel, ov.p.sel)
	}
}

// ↑ on the first anchor with text above it pages up before it wraps.
func TestOverviewSplitUpPagesAboveTheFirstAnchor(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModelOf(t, "THE TOP"+longTail(80)+"\n\nStart at [A](a.go:3), then [B](b.go:3).")
	ov := sv.left.doc()
	rows, _ := sv.geomOf(m, sv.left)
	top := ov.p.clampTop(ov.p.sel, rows)
	if ov.ov.sel != 0 || top == 0 {
		t.Fatalf("sel %d top %d: the split opens on the first anchor, below the top", ov.ov.sel, top)
	}
	m = pressDrain(t, m, "up")
	if ov.ov.sel != 0 {
		t.Fatalf("↑ wrapped to anchor %d with text above", ov.ov.sel)
	}
	if now := ov.p.clampTop(ov.p.sel, rows); now >= top {
		t.Fatalf("↑ did not scroll (top %d → %d)", top, now)
	}
	_ = m
}

// In the wrap view ↓ past the last anchor wraps as soon as the end is on
// screen, never paging on to a pane holding only the last line.
func TestOverviewSplitDownWrapsOnceTheEndShowsInTheWrapView(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModelOf(t, "Start at [A](a.go:3), then [B](b.go:3)."+longTail(80)+"\n\nTHE END")
	ov := sv.left.doc()
	for range 4 {
		if ov.p.mode == modeWrap {
			break
		}
		m = pressDrain(t, m, "ctrl+w")
	}
	if ov.p.mode != modeWrap {
		t.Fatalf("mode %v: ctrl+w must reach the wrap view", ov.p.mode)
	}
	m = pressDrain(t, m, "down")
	if ov.ov.sel != 1 {
		t.Fatalf("sel %d, want the last anchor", ov.ov.sel)
	}
	rows, _ := sv.geomOf(m, sv.left)
	for range 40 {
		endShown := ov.p.lastVisible(rows) >= len(ov.p.lines)-1
		m = pressDrain(t, m, "down")
		if endShown {
			if ov.ov.sel != 0 {
				t.Fatalf("the end was on screen and ↓ paged on (top %d of %d)", ov.p.sel, len(ov.p.lines))
			}
			return
		}
		if ov.ov.sel != 1 {
			t.Fatalf("↓ wrapped with text below (top %d of %d)", ov.p.sel, len(ov.p.lines))
		}
	}
	t.Fatal("↓ never reached the end")
}

// ↓ on the last anchor scrolled out of view (home) walks the anchors as
// before: the page-through runs only from a window showing the anchor.
func TestOverviewSplitDownFromAScrolledAwayLastAnchorWraps(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModelOf(t, "THE TOP"+longTail(80)+"\n\nStart at [A](a.go:3), then [B](b.go:3)."+longTail(80))
	ov := sv.left.doc()
	m = pressDrain(t, m, "down")
	if ov.ov.sel != 1 {
		t.Fatalf("sel %d, want the last anchor", ov.ov.sel)
	}
	m = pressDrain(t, m, "home")
	m = pressDrain(t, m, "down")
	if ov.ov.sel != 0 {
		t.Fatalf("↓ paged from a window without the anchor: sel %d top %d", ov.ov.sel, ov.p.sel)
	}
}
