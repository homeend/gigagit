package tui

import (
	"testing"

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
