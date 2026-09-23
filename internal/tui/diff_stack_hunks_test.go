package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

// wtFiles builds a working-tree status: every path a tracked file with an
// unstaged modification, which is what the Files panel (and an unstaged stack)
// shows.
func wtFiles(paths ...string) []model.FileStatus {
	out := make([]model.FileStatus, len(paths))
	for i, p := range paths {
		out[i] = model.FileStatus{Path: p, Unstaged: 'M'}
	}
	return out
}

// hunkStackModel opens a working-tree stack over files (staged = the Staged
// section) through the real openStatusDiff door, then hands every fetchable
// file the rows the lazy queue would have brought. The reader starts on the
// section's first file.
func hunkStackModel(t *testing.T, files []model.FileStatus, staged bool) Model {
	t.Helper()
	m := tempPromptStore(t, diffModel())
	m.height, m.width = 20, 120
	m = m.withStatus(model.WorkingTreeStatus{Files: files})
	m = m.setStackedPref(true)
	first := -1
	for i, f := range files {
		if (staged && inStagedPanel(f)) || (!staged && inFilesPanel(f)) {
			first = i
			break
		}
	}
	if first < 0 {
		t.Fatal("fixture: no file belongs to the section under test")
	}
	u, _ := m.openStatusDiff(files[first], staged)
	mm := u.(Model)
	v := mm.diffLayer()
	if v == nil || v.stk == nil {
		t.Fatal("fixture: the stacked preference did not open a stack")
	}
	for i := range v.stk.files {
		if v.stk.files[i].conflict {
			continue
		}
		rows := sameRowsTUI(3, 1)
		v.stk.files[i].d, v.stk.files[i].load = diffViewWith(rows, blocksOf(rows)), stackLoaded
	}
	v.rebuild()
	return mm
}

// focusStackFile puts the cursor on path's header.
func focusStackFile(t *testing.T, m Model, path string) Model {
	t.Helper()
	v := m.diffLayer()
	for i := range v.stk.files {
		if v.stk.files[i].path == path {
			v.setCursorLine(v.stk.files[i].hdr, m.diffBodyRows())
			return m
		}
	}
	t.Fatalf("the stack has no file %q", path)
	return m
}

// H in a working-tree stack acts on the file under the CURSOR, not on whatever
// the Files panel happens to have selected behind the view.
func TestStackHStagesTheFileUnderTheCursor(t *testing.T) {
	t.Parallel()
	m := hunkStackModel(t, wtFiles("a.txt", "b.txt", "c.txt"), false)
	m = focusStackFile(t, m, "b.txt")

	f, staged, why := m.hunkFileHere()
	if why != "" {
		t.Fatalf("H refused a plain modified file: %q", why)
	}
	if f.Path != "b.txt" {
		t.Fatalf("H would stage %q; it acts on the cursor's file", f.Path)
	}
	if staged {
		t.Fatal("the unstaged section must open the STAGE picker, not the unstage one")
	}
	u, cmd := m.Update(keyMsg("H"))
	if cmd == nil {
		t.Fatal("H issued no command; it must read b.txt's two sides for the picker")
	}
	if u.(Model).statusMsg != "" {
		t.Fatalf("H posted a notice instead of opening the picker: %q", u.(Model).statusMsg)
	}
}

// The Staged section unstages: the same key, the other lane. A file not yet in
// HEAD is excluded — StageHunks can only set index content, never remove the
// entry, so such a file is unstaged whole (the Staged panel's own rule).
func TestStagedStackHUnstagesAndRefusesAStagedAddition(t *testing.T) {
	t.Parallel()
	m := hunkStackModel(t, []model.FileStatus{
		{Path: "b.txt", Staged: 'M'},
		{Path: "n.txt", Staged: 'A'},
	}, true)

	m = focusStackFile(t, m, "b.txt")
	f, staged, why := m.hunkFileHere()
	if why != "" || f.Path != "b.txt" || !staged {
		t.Fatalf("H on a staged modification: file=%q staged=%v why=%q", f.Path, staged, why)
	}

	m = focusStackFile(t, m, "n.txt")
	u, cmd := m.Update(keyMsg("H"))
	if cmd != nil {
		t.Fatal("H on a staged-A file must refuse, not open a picker")
	}
	if msg := u.(Model).statusMsg; !strings.Contains(msg, "unstaged whole") {
		t.Fatalf("no notice explaining the refusal: %q", msg)
	}
}

// Where hunks cannot apply, H says why instead of opening a picker over a file
// the staging op would reject (design D4: a conflict keeps its resolver door).
func TestHRefusesWhereHunksCannotApply(t *testing.T) {
	t.Parallel()
	m := hunkStackModel(t, []model.FileStatus{
		{Path: "a.txt", Unstaged: 'M'},
		{Path: "new.txt", Unstaged: '?', Kind: model.KindUntracked},
		{Path: "u.txt", Staged: 'U', Unstaged: 'U', Kind: model.KindUnmerged},
	}, false)

	for _, tc := range []struct{ file, want string }{
		{"new.txt", "staged whole"},
		{"u.txt", "resolver"},
	} {
		mm := focusStackFile(t, m, tc.file)
		u, cmd := mm.Update(keyMsg("H"))
		if cmd != nil {
			t.Fatalf("%s opened a picker; H must refuse", tc.file)
		}
		if msg := u.(Model).statusMsg; !strings.Contains(msg, tc.want) {
			t.Fatalf("%s: notice %q does not say %q", tc.file, msg, tc.want)
		}
	}

	// A commit stack has no working tree to stage into.
	cm := diffModel()
	cm.height, cm.width = 20, 120
	cm.diffNav = diffNavTree
	cm = cm.pushLayer(stackViewOf(t, sameRowsTUI(3, 1)))
	u, cmd := cm.Update(keyMsg("H"))
	if cmd != nil {
		t.Fatal("a commit stack must refuse H")
	}
	if msg := u.(Model).statusMsg; !strings.Contains(msg, "working-tree") {
		t.Fatalf("a commit stack's notice is %q", msg)
	}
}

// wtDiffModel opens a SINGLE-file working-tree diff (the stacked preference
// off) on path.
func wtDiffModel(t *testing.T, files []model.FileStatus, path string) Model {
	t.Helper()
	m := tempPromptStore(t, diffModel())
	m.height, m.width = 20, 120
	m = m.withStatus(model.WorkingTreeStatus{Files: files})
	m = m.setStackedPref(false)
	f, ok := m.statusFileOf(path)
	if !ok {
		t.Fatalf("fixture: %q is not in the status", path)
	}
	u, _ := m.openStatusDiff(f, false)
	mm := u.(Model)
	if v := mm.diffLayer(); v == nil || v.stk != nil {
		t.Fatal("fixture: want a single-file diff, not a stack")
	}
	return mm
}

// D6: a stack reconciles itself on every status write, but ONE file does not —
// nothing reloads an open working-tree diff today. So the staging round parks a
// reload and the next status write consumes it. Parked, not unconditional: a
// reload on every status write would throw the reader to the top of the file
// whenever a watch-driven refresh landed.
func TestSingleFileDiffReloadsAfterItsOwnStagingRound(t *testing.T) {
	t.Parallel()
	files := wtFiles("a.txt", "b.txt")
	m := wtDiffModel(t, files, "b.txt")

	if _, cmd := m.Update(keyMsg("H")); cmd == nil {
		t.Fatal("H must open the picker from a single-file working-tree diff too")
	}

	m = m.armHunkReload("b.txt", false)
	u, cmd := m.Update(statusRefreshedMsg{status: model.WorkingTreeStatus{Files: files}})
	nm := u.(Model)
	if cmd == nil {
		t.Fatal("the parked reload was never issued")
	}
	if nm.hunkReload != nil {
		t.Fatal("the reload must be consumed once, not re-fire on every refresh")
	}
	if nm.diffLayer() == nil {
		t.Fatal("the file is still unstaged: its diff must stay open")
	}

	// …and when the file left the section entirely, the diff closes instead of
	// showing a stale one.
	m2 := wtDiffModel(t, files, "b.txt").armHunkReload("b.txt", false)
	u2, _ := m2.Update(statusRefreshedMsg{status: model.WorkingTreeStatus{Files: wtFiles("a.txt")}})
	nm2 := u2.(Model)
	if nm2.diffLayer() != nil {
		t.Fatal("a fully staged file must close its diff, not leave a stale one open")
	}
	if !strings.Contains(nm2.statusMsg, "b.txt") {
		t.Fatalf("no notice naming the file that left: %q", nm2.statusMsg)
	}
}

// A stack arms nothing: reconcileStatusStack already re-reads the file in place,
// keeping the reader's position — a reload would blank the whole view.
func TestAStackArmsNoReload(t *testing.T) {
	t.Parallel()
	m := hunkStackModel(t, wtFiles("a.txt", "b.txt"), false)
	if m.armHunkReload("b.txt", false).hunkReload != nil {
		t.Fatal("a stack must not park a reload; its reconcile does the work")
	}
}

// A key nobody can see does not exist: H is advertised in the footer of every
// working-tree diff (which buys the column from [h/b] hist — both stay in ? and
// in ctrl+p) and carries a . menu row wherever it applies.
func TestHIsAdvertisedWhereItApplies(t *testing.T) {
	t.Parallel()
	for _, stacked := range []bool{false, true} {
		wt := diffHintFor(longScroll, stacked, true)
		if !strings.Contains(wt, "[H] hunks") {
			t.Fatalf("stacked=%v: the working-tree footer never offers H:\n%s", stacked, wt)
		}
		if w := lipgloss.Width(wt); w > 140 {
			t.Fatalf("stacked=%v: the working-tree hint is %d columns, the budget is 140: %q", stacked, w, wt)
		}
		if plain := diffHintFor(longScroll, stacked, false); strings.Contains(plain, "[H] hunks") {
			t.Fatalf("stacked=%v: a commit diff advertises a key that refuses there", stacked)
		}
	}

	m := hunkStackModel(t, wtFiles("a.txt"), false)
	if !hasRow(m, "diff-hunks") {
		t.Fatal("the . menu of a working-tree stack has no hunk-staging row")
	}
	sm := hunkStackModel(t, []model.FileStatus{{Path: "b.txt", Staged: 'M'}}, true)
	if r, ok := sm.diffHunkRow(); !ok || !strings.Contains(r.label, "Unstage") {
		t.Fatalf("the Staged section's menu row is %+v; it must say Unstage", r)
	}
}

// TestWorkingTreeStackReconcilesOnStatusWrite pins the behaviour plan 4c rests
// on instead of rebuilding: reconcileStatusStack already runs from withStatus —
// the one place the Files/Staged membership is derived — so after a staging
// round the stack keeps the reader's file, drops a file that left the section,
// and marks a re-read file STALE rather than emptying it (its rows stay on
// screen until the lazy queue reaches it). This PASSES on main; it is here so
// 4c cannot silently lose it.
func TestWorkingTreeStackReconcilesOnStatusWrite(t *testing.T) {
	t.Parallel()
	m := hunkStackModel(t, wtFiles("a.txt", "b.txt", "c.txt"), false)
	m = focusStackFile(t, m, "b.txt")
	v := m.diffLayer()
	if p := v.stk.files[v.curFile()].path; p != "b.txt" {
		t.Fatalf("fixture: the cursor is on %q, want b.txt", p)
	}

	// a.txt is staged whole: it leaves the unstaged section.
	m = m.withStatus(model.WorkingTreeStatus{Files: wtFiles("b.txt", "c.txt")})

	v = m.diffLayer()
	if got := stackPaths(v); len(got) != 2 || got[0] != "b.txt" || got[1] != "c.txt" {
		t.Fatalf("the stack holds %v, want the section without a.txt", got)
	}
	if p := v.stk.files[v.curFile()].path; p != "b.txt" {
		t.Fatalf("the reader moved to %q; the cursor keeps its file BY PATH", p)
	}
	if f := v.stk.files[0]; f.d == nil || f.load != stackStale {
		t.Fatalf("b.txt kept d=%v load=%v; a re-read file keeps its rows and goes stale", f.d != nil, f.load)
	}
}

// And when the section empties entirely the stack closes rather than showing an
// empty scroll — the web's reconcileStack does the same through enterFilesStage.
func TestWorkingTreeStackClosesWhenItsSectionEmpties(t *testing.T) {
	t.Parallel()
	m := hunkStackModel(t, wtFiles("a.txt"), false)
	if m.diffLayer() == nil {
		t.Fatal("fixture: the stack must be open")
	}
	m = m.withStatus(model.WorkingTreeStatus{Files: nil})
	if m.diffLayer() != nil {
		t.Fatal("an emptied section must close the stack, not leave an empty one open")
	}
}
