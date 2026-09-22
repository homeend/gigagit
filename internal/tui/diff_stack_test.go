package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

// blocksOf is the change-block start index of every run of non-Same rows —
// what a real domain.Diff hands the view as Result.Blocks.
func blocksOf(rows []textdiff.Row) []int {
	var out []int
	prev := true // treat the head as "previous row was Same"
	for i, r := range rows {
		same := r.Kind == textdiff.Same
		if !same && prev {
			out = append(out, i)
		}
		prev = same
	}
	return out
}

// stackViewOf builds a stacked view over len(rows) files named f0.go, f1.go…,
// each loaded with its rows — nil rows meaning "not loaded yet". Width 120.
func stackViewOf(t *testing.T, rows ...[]textdiff.Row) *diffView {
	t.Helper()
	stk := &diffStack{gen: 1, src: diffNavTree}
	for i, r := range rows {
		f := stackFile{path: fmt.Sprintf("f%d.go", i), status: "M"}
		if r != nil {
			f.d, f.load = diffViewWith(r, blocksOf(r)), stackLoaded
		}
		stk.files = append(stk.files, f)
	}
	v := &diffView{stk: stk, width: 120}
	v.rebuild()
	return v
}

// The stream is header, body…, header, … — and the jump blocks ARE the
// headers, which is what makes n/p step file to file.
func TestStackSpliceHeadersThenBodies(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(3, 1), nil, sameRowsTUI(2, 0))
	var kinds []lineKind
	for _, l := range v.lines {
		kinds = append(kinds, l.kind)
	}
	want := []lineKind{lineHeader, lineBody, lineBody, lineBody, lineHeader, linePlace, lineHeader, lineBody, lineBody}
	if !slices.Equal(kinds, want) {
		t.Fatalf("stream kinds = %v, want %v", kinds, want)
	}
	if !slices.Equal(v.blocks, []int{0, 4, 6}) {
		t.Fatalf("blocks must be the header indices, got %v", v.blocks)
	}
	if v.lines[7].file != 2 || v.stk.files[2].start != 6 {
		t.Fatalf("file index / start not stamped: line file %d, start %d", v.lines[7].file, v.stk.files[2].start)
	}
}

// A folded file contributes its header and nothing else, and is never fetched.
func TestStackCollapsedFileIsHeaderOnly(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(3, 1), sameRowsTUI(2, 0))
	v.stk.files[0].collapsed = true
	v.rebuild()
	if len(v.lines) != 1+1+2 {
		t.Fatalf("a folded file must contribute its header only: %d lines", len(v.lines))
	}
	if v.lines[0].kind != lineHeader || v.lines[1].kind != lineHeader {
		t.Fatal("the folded file's header must be followed straight by the next header")
	}
}

// Syntax runs are keyed by SOURCE LINE NUMBER, so line 1 of file 1 must paint
// with file 1's runs — never file 0's.
func TestStackTokensResolvePerFile(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(1), sameRowsTUI(1))
	v.stk.files[0].d.newTok = [][]syntax.Tok{{{Start: 0, End: 1, Class: syntax.Keyword}}}
	v.stk.files[1].d.newTok = [][]syntax.Tok{{{Start: 0, End: 1, Class: syntax.String}}}
	// lines: H0 B0 H1 B1 — line 3 is file 1's only body row.
	if v.lines[3].file != 1 {
		t.Fatalf("line 3 belongs to file %d", v.lines[3].file)
	}
	_, nw := v.toksFor(3)
	if got := tokAt(nw, 1); len(got) != 1 || got[0].Class != syntax.String {
		t.Fatalf("file 1 line 1 painted with %v", got)
	}
}

// The gutter is shared across the stack, so every file's numbers line up.
func TestStackGutterIsTheWidestLoadedFile(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(4), sameRowsTUI(1200))
	if got, want := v.gutter(), 4; got != want {
		t.Fatalf("gutter = %d, want %d (1200 lines)", got, want)
	}
}

// The screen shows a header per file (rename arrow included), a placeholder
// for what has not loaded, and names the file the cursor is in.
func TestStackRenderPaintsHeaderAndPlaceholder(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(2, 0), nil)
	m := diffModel()
	m.height, m.width = 20, 120
	v.stk.files[0].counted, v.stk.files[0].add, v.stk.files[0].del = true, 12, 3
	v.stk.files[1].oldPath, v.stk.files[1].status = "old.go", "R"
	m = m.pushLayer(v)
	out := m.renderDiffView()
	for _, want := range []string{"▾ M  f0.go", "+12 −3", "▾ R  old.go → f1.go", "(not loaded yet)", "file 1/2"} {
		if !strings.Contains(out, want) {
			t.Errorf("the stacked screen lacks %q:\n%s", want, out)
		}
	}
}

// j/k walk onto a file header (so a FOLDED file can be reached, and `-` can
// unfold it) but never onto a placeholder; the cursor has no ROW on a header,
// which is what makes notes / copy / e inert there.
func TestStackCursorStopsOnHeadersSkipsPlaceholders(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(1), nil, sameRowsTUI(1)) // H B H P H B
	v.setCursorLine(1, 10)
	v.moveCursor(1, 10)
	if v.curLine != 2 || v.lines[2].kind != lineHeader {
		t.Fatalf("j from a body row must land on the next header, got line %d", v.curLine)
	}
	if _, ok := v.cursorRow(); ok {
		t.Fatal("cursorRow must be false on a header")
	}
	v.moveCursor(1, 10)
	if v.curLine != 4 {
		t.Fatalf("j must skip the placeholder to the next header, got %d", v.curLine)
	}
	if got := v.curFile(); got != 2 {
		t.Fatalf("the cursor's file is %d, want 2", got)
	}
}

// The search matches body lines only, and a selection spanning files copies
// their text without the headers or placeholders between them.
func TestStackSearchAndSelectionSkipNonBody(t *testing.T) {
	t.Parallel()
	one := []textdiff.Row{{Kind: textdiff.Same, Left: "needle", Right: "needle", LeftNo: 1, RightNo: 1}}
	v := stackViewOf(t, one, nil, one)
	for _, sl := range v.searchLines() {
		if v.lines[sl.row].kind != lineBody {
			t.Fatalf("the search indexed a %v line", v.lines[sl.row].kind)
		}
	}
	v.lsel.start(0)
	v.curLine = len(v.lines) - 1
	if got := v.selectedLines(); !slices.Equal(got, []string{"needle", "needle"}) {
		t.Fatalf("the selection copied %q, want the two body lines only", got)
	}
}

// `e` opens the cursor's OWN file at a line of that file — never a line
// number borrowed from the next file down the stack.
func TestStackEditLineStaysInTheCursorFile(t *testing.T) {
	t.Parallel()
	del := []textdiff.Row{{Kind: textdiff.Del, Left: "gone", LeftNo: 7}} // no new-side number
	next := []textdiff.Row{{Kind: textdiff.Same, Left: "x", Right: "x", LeftNo: 90, RightNo: 90}}
	v := stackViewOf(t, del, next)
	v.setCursorLine(1, 10) // the Del row of file 0
	if got := v.editLine(); got != 0 {
		t.Fatalf("editLine = %d; file 0 has no new-side line, and file 1's 90 is not its own", got)
	}
}

// The queue asks for the files nearest what is being read, never a folded or
// conflicted one, and never more than the in-flight cap allows.
func TestWantLoadsNearestFirstCappedAtThree(t *testing.T) {
	t.Parallel()
	files := make([]stackFile, 8)
	for i := range files {
		files[i].start = i * 10
	}
	files[2].collapsed = true
	files[3].conflict = true
	// Reading at line 30: distances are 4→10, 5→20, 1→20 (a tie goes to the
	// file BELOW), then 0→30 and 6→30, which the cap of 3 cuts off.
	if got := wantLoads(files, 0, 60, 30, 0); !slices.Equal(got, []int{4, 5, 1}) {
		t.Fatalf("wantLoads = %v, want [4 5 1]", got)
	}
	if got := wantLoads(files, 0, 60, 30, 2); len(got) != 1 {
		t.Fatalf("two already in flight leaves room for one, got %v", got)
	}
	if got := wantLoads(files, 0, 60, 30, 3); got != nil {
		t.Fatalf("the cap must block every further load, got %v", got)
	}
	files[4].load = stackStale // a working-tree refresh asks for a re-read
	files[5].load = stackLoaded
	if got := wantLoads(files, 0, 60, 30, 0); !slices.Equal(got, []int{4, 1, 6}) {
		t.Fatalf("a stale file must be re-requested and a loaded one left alone: %v", got)
	}
}

// A file loading ABOVE the viewport must not move what the user is reading:
// the cursor keeps its row and its position on screen (design §6.2).
func TestStackLateLoadAboveViewportKeepsScreen(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, nil, sameRowsTUI(40, 5), sameRowsTUI(40, 5))
	m := diffModel()
	m.height, m.width = 12, 120
	m = m.pushLayer(v)
	dv := m.diffLayer()
	body := m.diffBodyRows()
	dv.setCursorLine(dv.stk.files[2].start+10, body)
	dv.alignCursor(alignCenter, body)
	row, hadRow := dv.cursorRow()
	if !hadRow {
		t.Fatal("the cursor must sit on a body row")
	}
	screen := dv.lineStart[dv.curLine] - dv.offset

	late := diffViewWith(sameRowsTUI(30, 3), []int{3})
	u, _ := m.Update(stackFileMsg{gen: dv.stk.gen, idx: 0, view: late})
	dv = u.(Model).diffLayer()
	r2, ok := dv.cursorRow()
	if !ok || r2.LeftNo != row.LeftNo || r2.RightNo != row.RightNo || dv.curFile() != 2 {
		t.Fatalf("the cursor moved: %+v → %+v (file %d)", row, r2, dv.curFile())
	}
	if s2 := dv.lineStart[dv.curLine] - dv.offset; s2 != screen {
		t.Fatalf("the cursor's screen row moved %d → %d", screen, s2)
	}
	if !dv.stk.files[0].counted || dv.stk.files[0].add == 0 {
		t.Fatal("an arrival must count its rows when numstat has not")
	}
}

// An answer owed to a stack that has since been rebuilt is dropped.
func TestStackStaleGenIsDropped(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, nil)
	m := diffModel()
	m.height, m.width = 12, 120
	m = m.pushLayer(v)
	u, _ := m.Update(stackFileMsg{gen: 99, idx: 0, view: diffViewWith(sameRowsTUI(2, 0), []int{0})})
	if u.(Model).diffLayer().stk.files[0].d != nil {
		t.Fatal("a stale generation must not land")
	}
}

// The title follows the cursor's file, which is how h / b / e / the copy rows
// keep acting on the file being read.
func TestStackTitleFollowsTheCursorFile(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(2), sameRowsTUI(2))
	v.setCursorLine(v.stk.files[1].start, 10)
	v.syncStackTitle()
	if v.title != "f1.go" {
		t.Fatalf("title = %q, want f1.go", v.title)
	}
}
