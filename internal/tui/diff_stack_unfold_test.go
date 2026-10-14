package tui

import (
	"fmt"
	"testing"
)

// bigStackModel opens a stack over 101 files (past stackCollapseOver) on the
// first one, so every other file starts folded by the stack itself.
func bigStackModel(t *testing.T) Model {
	t.Helper()
	m := tempPromptStore(t, treeDiffModel(1))
	m.height, m.width = 24, 120
	lines := []contentLine{{text: "dir/", heading: true}}
	for i := 0; i < 101; i++ {
		p := fmt.Sprintf("f%03d.go", i)
		lines = append(lines, contentLine{text: "  M " + p, path: p, status: "M"})
	}
	m.filesView = &contentPopup{lines: lines, sel: 1}
	m = m.setStackedPref(true)
	u, _ := m.openDiffForFileLine(lines[1])
	m = u.(Model)
	if v := m.diffLayer(); v == nil || v.stk == nil || len(v.stk.files) != 101 {
		t.Fatal("the whole list must stack")
	}
	return m
}

// visibleHeaderFiles are the files whose header is on screen.
func visibleHeaderFiles(m Model) map[int]bool {
	v, body := m.diffLayer(), m.diffBodyRows()
	out := map[int]bool{}
	for r := v.offset; r < v.offset+body && r < len(v.disp); r++ {
		if li := v.disp[r].line; v.lines[li].kind == lineHeader {
			out[v.lines[li].file] = true
		}
	}
	return out
}

func pressDiff(t *testing.T, m Model, k string) Model {
	t.Helper()
	u, _ := m.diffLayer().update(m, key(k))
	return u
}

// Paging through a stack that opened folded unfolds every file the page
// brings into view, so it loads like a small stack's would. What was already
// on screen when the stack opened stays as it opened, and a file never shown
// stays folded.
func TestStackPagingUnfoldsTheFilesItBringsIntoView(t *testing.T) {
	t.Parallel()
	m := bigStackModel(t)
	atOpen := visibleHeaderFiles(m)
	m = pressDiff(t, m, "pgdown")
	v := m.diffLayer()
	unfolded := 0
	for i := 1; i < len(v.stk.files); i++ {
		f := v.stk.files[i]
		if atOpen[i] && !f.collapsed {
			t.Fatalf("file %d was on screen when the stack opened: it must stay folded", i)
		}
		if !f.collapsed {
			unfolded++
		}
	}
	if unfolded == 0 {
		t.Fatal("pgdown brought folded files into view and unfolded none of them")
	}
	if !v.stk.files[100].collapsed {
		t.Fatal("a file never shown must stay folded")
	}
	for i := range visibleHeaderFiles(m) {
		if f := v.stk.files[i]; i > 0 && !atOpen[i] && f.collapsed {
			t.Fatalf("file %d is on screen after pgdown and still folded", i)
		}
	}
}

// The cursor walking onto a folded header unfolds that file.
func TestStackCursorOntoAFoldedHeaderUnfoldsIt(t *testing.T) {
	t.Parallel()
	m := bigStackModel(t)
	m = pressDiff(t, m, "j")
	v := m.diffLayer()
	if v.lines[v.curLine].kind != lineHeader || v.curFile() != 1 {
		t.Fatalf("j must stop on f001's header (file %d, kind %v)", v.curFile(), v.lines[v.curLine].kind)
	}
	if v.stk.files[1].collapsed {
		t.Fatal("the cursor landed on a folded header: the file must unfold")
	}
}

// A file the reader folded by hand stays folded when it scrolls back in.
func TestStackFileFoldedByHandStaysFolded(t *testing.T) {
	t.Parallel()
	m := bigStackModel(t)
	m = pressDiff(t, m, "j") // onto f001's header: it unfolds
	m = pressDiff(t, m, "-") // …and the reader folds it again
	v := m.diffLayer()
	if !v.stk.files[1].collapsed {
		t.Fatal("- must fold the file under the cursor")
	}
	m = pressDiff(t, m, "pgdown")
	m = pressDiff(t, m, "pgup")
	m = pressDiff(t, m, "j")
	m = pressDiff(t, m, "k")
	if !m.diffLayer().stk.files[1].collapsed {
		t.Fatal("a file folded by hand must stay folded when it comes back into view")
	}
}
