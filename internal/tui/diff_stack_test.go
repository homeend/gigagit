package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
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
	want := []lineKind{
		lineHeader, lineRule, lineBody, lineBody, lineBody, // file 0 (no blank line above the first file)
		lineGap, lineHeader, lineRule, linePlace, // file 1, not loaded
		lineGap, lineHeader, lineRule, lineBody, lineBody, // file 2
	}
	if !slices.Equal(kinds, want) {
		t.Fatalf("stream kinds = %v, want %v", kinds, want)
	}
	if !slices.Equal(v.blocks, []int{0, 6, 10}) {
		t.Fatalf("blocks must be the header indices, got %v", v.blocks)
	}
	if v.lines[12].file != 2 || v.stk.files[2].start != 9 || v.stk.files[2].hdr != 10 {
		t.Fatalf("file index / start / hdr not stamped: line file %d, start %d, hdr %d",
			v.lines[12].file, v.stk.files[2].start, v.stk.files[2].hdr)
	}
}

// A folded file contributes its header and nothing else, and is never fetched.
func TestStackCollapsedFileIsHeaderOnly(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(3, 1), sameRowsTUI(2, 0))
	v.stk.files[0].collapsed = true
	v.rebuild()
	// file 0 folded: header + rule; file 1: blank, header, rule, two rows.
	if len(v.lines) != 2+3+2 {
		t.Fatalf("a folded file must contribute its header and rule only: %d lines", len(v.lines))
	}
	if v.lines[0].kind != lineHeader || v.lines[1].kind != lineRule || v.lines[2].kind != lineGap {
		t.Fatal("a folded file must run straight into the next file's blank line")
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
	// H R B | gap H R P | gap H R B
	v := stackViewOf(t, sameRowsTUI(1), nil, sameRowsTUI(1))
	v.setCursorLine(2, 10) // file 0's only body row
	v.moveCursor(1, 10)
	if v.lines[v.curLine].kind != lineHeader || v.curFile() != 1 {
		t.Fatalf("j from a body row must land on the next file's header, got line %d kind %v",
			v.curLine, v.lines[v.curLine].kind)
	}
	if _, ok := v.cursorRow(); ok {
		t.Fatal("cursorRow must be false on a header")
	}
	v.moveCursor(1, 10)
	if v.lines[v.curLine].kind != lineHeader || v.curFile() != 2 {
		t.Fatalf("j must skip the rule, the placeholder and the blank line to the next header, got line %d kind %v",
			v.curLine, v.lines[v.curLine].kind)
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

// tempPromptStore gives a model its own machine-local memory, so a test never
// reads or writes the real one.
func tempPromptStore(t *testing.T, m Model) Model {
	t.Helper()
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	// New() read the MACHINE's store before this one replaced it, so the
	// session flag has to come from the temp store too — otherwise a test
	// passes or fails by whether the developer left the stacked view on.
	m.diffStacked = m.promptStore.StackedDiff()
	return m
}

// S stacks the open diff around the file being read — keeping that file, its
// cursor line and the rows already loaded — and remembers the choice. S again
// returns to the single file.
func TestSFlipsSingleToStackKeepingTheFile(t *testing.T) {
	t.Parallel()
	m := tempPromptStore(t, treeDiffModelBlocks(2)) // the tree's b.go
	v := m.diffLayer()
	v.title = "b.go"
	v.setCursorLine(20, m.diffBodyRows())

	u, cmd := m.Update(keyMsg("S"))
	mm := u.(Model)
	sv := mm.diffLayer()
	if sv.stk == nil || len(sv.stk.files) != 3 {
		t.Fatalf("S must stack the three tree files, got %v", sv.stk)
	}
	if sv.curFile() != 1 || sv.title != "b.go" {
		t.Fatalf("the stack must sit on b.go: file %d, title %q", sv.curFile(), sv.title)
	}
	if r, ok := sv.cursorRow(); !ok || r.RightNo != 21 {
		t.Fatalf("the seeded file keeps its cursor line, got %+v (ok %v)", r, ok)
	}
	if !mm.diffStacked || !mm.promptStore.StackedDiff() {
		t.Fatal("S must set and persist the preference")
	}
	if cmd == nil {
		t.Fatal("opening a stack must start loading its neighbours")
	}

	u2, _ := mm.Update(keyMsg("S"))
	back := u2.(Model)
	if back.diffLayer().stk != nil || back.diffLayer().title != "b.go" {
		t.Fatalf("S again must return to the single view on b.go, got %q", back.diffLayer().title)
	}
	if back.diffStacked || back.filesView.sel != 2 {
		t.Fatal("leaving a stack must clear the pref and point the list at the file")
	}
}

// n/p step header to header; - folds the cursor's file; _ folds all, then
// unfolds all.
func TestStackNPStepHeadersAndFoldKeys(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.height, m.width = 30, 120
	m = m.pushLayer(stackViewOf(t, sameRowsTUI(3, 1), sameRowsTUI(3, 1), sameRowsTUI(3, 1)))
	for _, want := range []int{1, 2} {
		u, _ := m.Update(keyMsg("n"))
		m = u.(Model)
		v := m.diffLayer()
		if v.curFile() != want || v.lines[v.curLine].kind != lineHeader {
			t.Fatalf("n must land on file %d's header, got file %d kind %v", want, v.curFile(), v.lines[v.curLine].kind)
		}
	}
	u, _ := m.Update(keyMsg("-"))
	m = u.(Model)
	if !m.diffLayer().stk.files[2].collapsed {
		t.Fatal("- must fold the cursor's file")
	}
	u, _ = m.Update(keyMsg("_"))
	m = u.(Model)
	for i, f := range m.diffLayer().stk.files {
		if !f.collapsed {
			t.Fatalf("_ with some unfolded must fold all (file %d is open)", i)
		}
	}
	u, _ = m.Update(keyMsg("_"))
	for i, f := range u.(Model).diffLayer().stk.files {
		if f.collapsed {
			t.Fatalf("_ with all folded must unfold all (file %d stayed folded)", i)
		}
	}
}

// home / end go to the ends of the WHOLE stack and never prime a file step —
// every file of the list is already on screen.
func TestStackHomeEndNeverArmAFileStep(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.height, m.width = 12, 120
	m = m.pushLayer(stackViewOf(t, sameRowsTUI(20, 2), sameRowsTUI(20, 2)))
	m.diffNav = diffNavTree
	tag := m.diffTag
	u, _ := m.Update(keyMsg("end"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("end"))
	m = u.(Model)
	v := m.diffLayer()
	if v.fileArm != fileArmNone || m.diffTag != tag {
		t.Fatalf("end must not arm a file step (arm %d, tag %q)", v.fileArm, m.diffTag)
	}
	if v.curFile() != 1 {
		t.Fatalf("end must reach the last file, got %d", v.curFile())
	}
	u, _ = m.Update(keyMsg("home"))
	if got := u.(Model).diffLayer().curFile(); got != 0 {
		t.Fatalf("home must reach the first file, got %d", got)
	}
}

// A stack whose files carry no address (every two-sided compare) leaves the
// notes keys inert, exactly as the single-file view does for the same file —
// the stack inherits each file's own addressability, it does not add a rule.
func TestStackNotesKeysStayInertWithoutAnAddress(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.height, m.width = 20, 120
	m = m.pushLayer(stackViewOf(t, sameRowsTUI(4, 1), sameRowsTUI(4, 1)))
	for _, k := range []string{"c", "E", "R"} {
		u, _ := m.Update(keyMsg(k))
		mm := u.(Model)
		if _, isNote := mm.topLayer().(*notePopup); isNote {
			t.Fatalf("%q must not open a note surface on an unaddressed file", k)
		}
	}
}

// Past the threshold a stack opens folded — except the file that was opened.
func TestStackOver100OpensFoldedExceptTheOpenedFile(t *testing.T) {
	t.Parallel()
	m := tempPromptStore(t, treeDiffModel(1))
	m.height, m.width = 24, 120
	lines := []contentLine{{text: "dir/", heading: true}}
	for i := 0; i < 101; i++ {
		p := fmt.Sprintf("f%03d.go", i)
		lines = append(lines, contentLine{text: "  M " + p, path: p, status: "M"})
	}
	m.filesView = &contentPopup{lines: lines, sel: 51} // f050.go
	m = m.setStackedPref(true)
	u, _ := m.openDiffForFileLine(lines[51]) // the files view's enter on f050.go
	v := u.(Model).diffLayer()
	if v.stk == nil || len(v.stk.files) != 101 {
		t.Fatalf("the whole list must stack, got %v files", len(v.stk.files))
	}
	for i, f := range v.stk.files {
		if want := i != 50; f.collapsed != want {
			t.Fatalf("file %d collapsed = %v, want %v", i, f.collapsed, want)
		}
	}
	if v.curFile() != 50 {
		t.Fatalf("the stack must open on the file that was picked, got %d", v.curFile())
	}
}

// With no list behind the diff (a picker compare) S says so and changes nothing.
func TestSInertWithoutAList(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.height, m.width = 20, 120
	m = tempPromptStore(t, m)
	m = m.pushLayer(diffViewWith(sameRowsTUI(4, 1), []int{1}))
	m.diffNav = diffNavNone
	u, cmd := m.Update(keyMsg("S"))
	mm := u.(Model)
	if mm.diffLayer().stk != nil || cmd != nil {
		t.Fatal("S must not stack a diff with no source list")
	}
	if mm.diffNotice == "" || mm.diffStacked {
		t.Fatalf("S must say why and leave the pref alone (notice %q, pref %v)", mm.diffNotice, mm.diffStacked)
	}
}

// statusStackModel opens a stack over the unstaged section of three files,
// one of them conflicted.
func statusStackModel(t *testing.T) Model {
	t.Helper()
	m := tempPromptStore(t, diffModel())
	m.height, m.width = 20, 120
	m = m.withStatus(model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "a.txt", Unstaged: 'M'},
		{Path: "b.txt", Unstaged: 'M'},
		{Path: "c.txt", Staged: 'U', Unstaged: 'U', Kind: model.KindUnmerged},
	}})
	m = m.setStackedPref(true)
	u, _ := m.openStatusDiff(m.status.Files[0], false)
	return u.(Model)
}

// A working-tree stack is ONE section, and a conflicted file joins it as a
// header with the resolver behind enter — never as a diff.
func TestStatusStackIsTheSectionWithConflictsHeaderOnly(t *testing.T) {
	t.Parallel()
	m := statusStackModel(t)
	v := m.diffLayer()
	if v.stk == nil || len(v.stk.files) != 3 || v.stk.staged {
		t.Fatalf("the unstaged section must stack whole, got %+v", v.stk)
	}
	if !v.stk.files[2].conflict {
		t.Fatal("the unmerged file must be marked as a conflict")
	}
	if got := wantLoads(v.stk.files, 0, 1<<30, 0, 0); slices.Contains(got, 2) {
		t.Fatalf("a conflicted file must never be fetched, queue = %v", got)
	}
	v.setCursorLine(v.stk.files[2].start, m.diffBodyRows())
	u, cmd := m.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("enter on a conflicted file must open the resolver")
	}
	if u.(Model).diffLayer().stk == nil {
		t.Fatal("the stack must stay open behind the resolver load")
	}
}

// A live status refresh follows the section: gone files drop out, new ones
// appear, a file already read is kept on screen but marked for a re-read, and
// the cursor stays on its own file.
func TestStatusStackReconcilesOnRefresh(t *testing.T) {
	t.Parallel()
	m := statusStackModel(t)
	v := m.diffLayer()
	v.stk.files[1].d, v.stk.files[1].load = diffViewWith(sameRowsTUI(4, 1), []int{1}), stackLoaded
	v.rebuild()
	v.setCursorLine(v.stk.files[1].start+2, m.diffBodyRows())
	v.syncStackTitle()
	gen := v.stk.gen

	m = m.withStatus(model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "b.txt", Unstaged: 'M'},
		{Path: "d.txt", Unstaged: 'M'},
	}})
	v = m.diffLayer()
	if v == nil || v.stk == nil {
		t.Fatal("the stack must survive a refresh that still has files")
	}
	if len(v.stk.files) != 2 || v.stk.files[0].path != "b.txt" || v.stk.files[1].path != "d.txt" {
		t.Fatalf("the stack must follow the section, got %v", stackPaths(v))
	}
	if v.stk.files[0].d == nil || v.stk.files[0].load != stackStale {
		t.Fatalf("a read file must stay on screen and be marked stale, got %v", v.stk.files[0].load)
	}
	if v.stk.gen == gen {
		t.Fatal("a rebuilt stack must take a new generation")
	}
	if v.curFile() != 0 || v.title != "b.txt" {
		t.Fatalf("the cursor must stay on b.txt, got file %d title %q", v.curFile(), v.title)
	}

	// The section empties: the view closes back to the list.
	m = m.withStatus(model.WorkingTreeStatus{})
	if m.diffLayer() != nil {
		t.Fatal("an emptied section must close the stacked view")
	}
}

func stackPaths(v *diffView) []string {
	var out []string
	for _, f := range v.stk.files {
		out = append(out, f.path)
	}
	return out
}

// The counts reach the headers from one numstat, before any body has loaded.
func TestStackCountsArriveBeforeBodies(t *testing.T) {
	t.Parallel()
	m := statusStackModel(t)
	v := m.diffLayer()
	u, _ := m.Update(stackStatMsg{gen: v.stk.gen, stats: []model.DiffStat{
		{Path: "a.txt", Added: 2, Deleted: 1},
		{Path: "b.txt", Binary: true},
	}})
	m = u.(Model)
	v = m.diffLayer()
	if f := v.stk.files[0]; !f.counted || f.add != 2 || f.del != 1 {
		t.Fatalf("a.txt counts = +%d −%d (counted %v)", f.add, f.del, f.counted)
	}
	if !v.stk.files[1].bin {
		t.Fatal("b.txt must be marked binary")
	}
	out := m.renderDiffView()
	if !strings.Contains(out, "+2 −1") || !strings.Contains(out, i18n.T("bin")) {
		t.Fatalf("the headers must show the counts with no body loaded:\n%s", out)
	}
	// A stack rebuilt meanwhile ignores the old read.
	u2, _ := m.Update(stackStatMsg{gen: v.stk.gen + 99, stats: []model.DiffStat{{Path: "a.txt", Added: 9}}})
	if got := u2.(Model).diffLayer().stk.files[0].add; got != 2 {
		t.Fatalf("a stale numstat must be dropped, add = %d", got)
	}
}

// The footer advertises the stacked view in the single-file line, and the
// stack's own keys once it is open (memory rule: help AND footer).
func TestDiffFooterAdvertisesStack(t *testing.T) {
	t.Parallel()
	single := diffHintFor(longScroll, false, false)
	if !strings.Contains(single, "[S] stack") {
		t.Errorf("the single-file footer must advertise S: %q", single)
	}
	stacked := diffHintFor(longScroll, true, false)
	for _, want := range []string{"[n/p] file", "[-/_] fold", "[J] files", "[S] single", "[esc] back"} {
		if !strings.Contains(stacked, want) {
			t.Errorf("the stacked footer lacks %q: %q", want, stacked)
		}
	}
}

// …and so does the ? help.
func TestHelpAdvertisesStack(t *testing.T) {
	t.Parallel()
	lines := helpFor(i18n.T("Diff view (enter)"), diffHintFor(longScroll, false, false))
	var text strings.Builder
	for _, l := range lines {
		text.WriteString(l.text)
		text.WriteString("\n")
	}
	for _, want := range []string{"stacked view", "-/_", "J", "enter (stacked)"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("the diff help must cover %q", want)
		}
	}
}

// The . menu carries the toggle wherever a stackable diff is open, and the
// fold / jump rows inside a stack.
func TestDotMenuHasStackRows(t *testing.T) {
	t.Parallel()
	m := tempPromptStore(t, treeDiffModelBlocks(2))
	if !rowHasID(availableActions(m), "stack-toggle") {
		t.Fatal("a stackable diff must offer the toggle row")
	}
	u, _ := m.Update(keyMsg("S"))
	rows := availableActions(u.(Model))
	for _, id := range []string{"stack-toggle", "stack-fold", "stack-fold-all", "stack-jump"} {
		if !rowHasID(rows, id) {
			t.Errorf("a stack's . menu lacks %q", id)
		}
	}
	// e keeps its menu row now that the footer no longer names it.
	if !rowHasID(availableActions(m), "diff-edit-at-line") {
		t.Error("the editor row must stay in the . menu")
	}
}

// J opens the file list, and picking a row scrolls to (and unfolds) that file.
func TestStackJumpMenuGoesToTheFile(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.height, m.width = 20, 120
	m = m.pushLayer(stackViewOf(t, sameRowsTUI(4, 1), sameRowsTUI(4, 1), sameRowsTUI(4, 1)))
	m.diffLayer().stk.files[2].collapsed = true
	m.diffLayer().rebuild()
	u, _ := m.Update(keyMsg("J"))
	mm := u.(Model)
	if mm.actionMenu == nil || len(mm.actionMenu.rows) != 3 {
		t.Fatalf("J must list the stack's three files, got %v", mm.actionMenu)
	}
	nm, _ := mm.actionMenu.rows[2].run(mm)
	v := nm.(Model).diffLayer()
	if v.curFile() != 2 || v.stk.files[2].collapsed {
		t.Fatalf("picking a folded file must go to it and unfold it (file %d, collapsed %v)", v.curFile(), v.stk.files[2].collapsed)
	}
}

// Flipping to a stack must keep the file's ROWS, not just its name: the seed
// is usually the live layer, which the stack is about to be written into.
func TestStackSeedKeepsTheFilesRows(t *testing.T) {
	t.Parallel()
	m := tempPromptStore(t, treeDiffModelBlocks(2))
	u, _ := m.Update(keyMsg("S"))
	v := u.(Model).diffLayer()
	f := v.stk.files[v.curFile()]
	if f.d == nil || f.d.stk != nil || len(f.d.full) == 0 {
		t.Fatalf("the seeded file must keep its own rows, got %+v", f.d)
	}
	if got := len(v.lines) - len(v.stk.files); got < 60 {
		t.Fatalf("the seeded file's body must be in the stream, %d body lines", got)
	}
}

// The file-step cue belongs to the single-file view: in a stack every file is
// already on screen.
func TestStackShowsNoFileStepCue(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.height, m.width = 20, 120
	m = m.pushLayer(stackViewOf(t, sameRowsTUI(4, 1), sameRowsTUI(4, 1)))
	m.diffNav = diffNavTree
	if cue := m.boundaryCue(); cue != "" {
		t.Fatalf("a stack must not advertise a file step, got %q", cue)
	}
}

// Stepping with n renames the view: the header, h/b/e and the copy rows all
// read v.title, so it must name the file the cursor moved to.
func TestStackTitleFollowsNAndJK(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.height, m.width = 20, 120
	m = m.pushLayer(stackViewOf(t, sameRowsTUI(4, 1), sameRowsTUI(4, 1)))
	u, _ := m.Update(keyMsg("n"))
	if got := u.(Model).diffLayer().title; got != "f1.go" {
		t.Fatalf("after n the title is %q, want f1.go", got)
	}
	u2, _ := u.(Model).Update(keyMsg("k"))
	if got := u2.(Model).diffLayer().title; got != "f0.go" {
		t.Fatalf("after k back into file 0 the title is %q, want f0.go", got)
	}
}

// f (changed lines only) and ctrl+w (long-line mode) rebuild the stream. In a
// stack the cursor must come back to the SAME file — line numbers repeat
// across files, so re-finding by number could land in another file entirely.
func TestStackFoldAndWrapKeepTheCursorsFile(t *testing.T) {
	t.Parallel()
	// Two files whose line numbers overlap exactly.
	m := diffModel()
	m.height, m.width = 20, 120
	m = m.pushLayer(stackViewOf(t, sameRowsTUI(30, 12), sameRowsTUI(30, 12)))
	v := m.diffLayer()
	v.setCursorLine(v.stk.files[1].start+13, m.diffBodyRows())
	v.syncStackTitle()
	before, _ := v.cursorRow()

	for _, key := range []string{"f", "ctrl+w"} {
		u, _ := m.Update(keyMsg(key))
		m = u.(Model)
		v = m.diffLayer()
		if v.curFile() != 1 || v.title != "f1.go" {
			t.Fatalf("%s moved the cursor to file %d (%q)", key, v.curFile(), v.title)
		}
		if r, ok := v.cursorRow(); ok && r.RightNo != before.RightNo {
			t.Fatalf("%s moved the cursor to line %d, want %d", key, r.RightNo, before.RightNo)
		}
	}
}

// Each file is framed: a blank line above its header (except the first file,
// which has nothing above it) and a full-width rule under it.
func TestStackFramesEachHeader(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(2, 0), sameRowsTUI(2, 0))
	m := diffModel()
	m.height, m.width = 20, 80
	m = m.pushLayer(v)
	lines := strings.Split(m.renderDiffView(), "\n")
	// line 0 is the view's own header; the stack starts at line 1.
	if !strings.Contains(lines[1], "▾ M  f0.go") {
		t.Fatalf("the first file must start at the top with no blank line: %q", lines[1])
	}
	rule := strings.Repeat("─", 80)
	if !strings.Contains(lines[2], rule) {
		t.Fatalf("a rule must run under the header, the full width: %q", lines[2])
	}
	// file 0 has two rows, then the blank line, then file 1's header + rule.
	if strings.TrimSpace(lines[5]) != "" {
		t.Fatalf("a blank line must open the next file: %q", lines[5])
	}
	if !strings.Contains(lines[6], "▾ M  f1.go") || !strings.Contains(lines[7], rule) {
		t.Fatalf("the next file must read blank, header, rule: %q / %q", lines[6], lines[7])
	}
}

// A conflicted file's header shows no +/− counts: its body is the resolver
// line, not a diff, so a numstat total there names something you cannot read.
func TestStackConflictHeaderShowsNoCounts(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, nil)
	f := &v.stk.files[0]
	f.conflict, f.status, f.path = true, "U", "c.go"
	f.add, f.del, f.counted = 4, 0, true
	v.rebuild()
	m := diffModel()
	m.height, m.width = 24, 120
	m = m.pushLayer(v)
	row := m.stackRow(v, dRow{line: f.hdr, kind: lineHeader, file: 0}, 120, false)
	if strings.Contains(row, "+4") || strings.Contains(row, "−0") {
		t.Fatalf("a conflicted header must show no counts: %q", row)
	}
	if !strings.Contains(row, "c.go") {
		t.Fatalf("the header must still name the file: %q", row)
	}
}

// Stacked, n/p step FILES, so the change-to-change walk inside one file moves
// to the ctrl-arrows — and it stops at the file's ends rather than spilling
// into the neighbouring file.
func TestStackCtrlArrowsStepChangesInsideTheFile(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(12, 2, 8), sameRowsTUI(12, 3))
	m := diffModel()
	m.height, m.width = 30, 120
	m = m.pushLayer(v)
	v.rebuild()
	body := m.diffBodyRows()
	v.setCursorLine(v.stk.files[0].hdr, body)

	u, _ := m.Update(keyMsg("ctrl+down"))
	mm := u.(Model)
	nv := mm.diffLayer()
	if nv.curFile() != 0 {
		t.Fatalf("ctrl+down left file 0 (now in %d)", nv.curFile())
	}
	first := nv.curLine
	if r, ok := nv.cursorRow(); !ok || r.Kind == textdiff.Same {
		t.Fatalf("ctrl+down must land on a change, got %+v ok=%v", r, ok)
	}
	u2, _ := mm.Update(keyMsg("ctrl+down"))
	mm = u2.(Model)
	nv = mm.diffLayer()
	if nv.curLine <= first {
		t.Fatalf("the second ctrl+down did not advance (%d → %d)", first, nv.curLine)
	}
	if nv.curFile() != 0 {
		t.Fatalf("ctrl+down stepped into file %d; n/p step files, ctrl-arrows stay in one", nv.curFile())
	}
	// past the file's last change it stays put and says so
	u3, _ := mm.Update(keyMsg("ctrl+down"))
	mm = u3.(Model)
	if mm.diffLayer().curFile() != 0 {
		t.Fatal("ctrl+down must not spill into the next file")
	}
	if mm.diffNotice == "" {
		t.Fatal("a key that cannot move must say why")
	}
}
