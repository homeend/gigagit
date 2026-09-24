package tui

import (
	"context"
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func wtDoc(path string) *openFile { return newOpenFile(fileSource{kind: srcWorktree}, path) }

func regPaths(r *openFilesReg, wt string) []string {
	var out []string
	for _, d := range r.list(wt) {
		out = append(out, d.path)
	}
	return out
}

func noneShown(*openFile) bool { return false }

func TestOpenFilesRegOrderAndFind(t *testing.T) {
	t.Parallel()
	r := &openFilesReg{}
	a, b := wtDoc("a"), wtDoc("b")
	r.touch("wt", a, noneShown)
	r.touch("wt", b, noneShown)
	if got := fmt.Sprint(regPaths(r, "wt")); got != "[b a]" {
		t.Fatalf("order = %s, want [b a]", got)
	}
	r.touch("wt", a, noneShown)
	if got := fmt.Sprint(regPaths(r, "wt")); got != "[a b]" {
		t.Fatalf("after re-touch = %s, want [a b]", got)
	}
	if r.find("wt", wtDoc("b").key()) != b || r.find("wt", "nope") != nil {
		t.Fatal("find by key failed")
	}
}

func TestOpenFilesRegEvictsTheOldestNotShown(t *testing.T) {
	t.Parallel()
	r := &openFilesReg{}
	first := wtDoc("f0")
	r.touch("wt", first, noneShown)
	for i := 1; i < maxOpenFiles; i++ {
		if ev := r.touch("wt", wtDoc(fmt.Sprintf("f%d", i)), noneShown); ev != nil {
			t.Fatalf("evicted %s below the cap", ev.path)
		}
	}
	shownFirst := func(d *openFile) bool { return d == first }
	ev := r.touch("wt", wtDoc("new"), shownFirst)
	if ev == nil || ev.path != "f1" {
		t.Fatalf("evicted %v, want f1 (f0 is on screen)", ev)
	}
	if n := len(r.list("wt")); n != maxOpenFiles {
		t.Fatalf("len = %d, want %d", n, maxOpenFiles)
	}
}

func TestOpenFilesRegRemoveAndWorktrees(t *testing.T) {
	t.Parallel()
	var nilReg *openFilesReg
	if nilReg.list("x") != nil || nilReg.find("x", "k") != nil {
		t.Fatal("a nil registry must read as empty")
	}
	r := &openFilesReg{}
	a := wtDoc("a")
	r.touch("one", a, noneShown)
	r.touch("two", wtDoc("b"), noneShown)
	if fmt.Sprint(regPaths(r, "one")) != "[a]" || fmt.Sprint(regPaths(r, "two")) != "[b]" {
		t.Fatalf("worktrees mixed: %v / %v", regPaths(r, "one"), regPaths(r, "two"))
	}
	r.remove("one", a)
	if len(r.list("one")) != 0 {
		t.Fatal("remove left the doc listed")
	}
}

func viewersOnStack(m Model) int {
	n := 0
	if m.layers != nil {
		for _, l := range m.layers.entries {
			if _, ok := l.(*fileViewer); ok {
				n++
			}
		}
	}
	return n
}

// Opening an open file reuses it: one entry, one frame, the reader's place
// kept — or the asked line when there is one.
func TestOpenFileViewerReusesTheOpenFile(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	var cmd tea.Cmd
	m, cmd = m.openFileViewer("a.txt", 0)
	m = pumpAll(t, m, cmd)
	fv := layerOf[*fileViewer](m)
	fv.p.cur = 3
	m, cmd = m.openFileViewer("a.txt", 0)
	m = pumpAll(t, m, cmd)
	if n := len(m.openFiles.list(m.currentWorktree)); n != 1 || viewersOnStack(m) != 1 {
		t.Fatalf("entries=%d viewers=%d, want 1 and 1", n, viewersOnStack(m))
	}
	if layerOf[*fileViewer](m).openFile != fv.openFile || fv.p.cur != 3 {
		t.Fatalf("reuse lost the place: cur=%d", fv.p.cur)
	}
	m, cmd = m.openFileViewer("a.txt", 2)
	m = pumpAll(t, m, cmd)
	if fv.p.cur != 1 {
		t.Fatalf("cur=%d, want 1 (line 2)", fv.p.cur)
	}
}

func TestTheTwentyFirstOpenDropsTheOldest(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	for i := 0; i <= maxOpenFiles; i++ {
		m, _ = m.openFileViewer(fmt.Sprintf("f%02d.txt", i), 0)
		m = m.popLayer() // each one goes to the background
	}
	l := m.openFiles.list(m.currentWorktree)
	if len(l) != maxOpenFiles || m.openFiles.find(m.currentWorktree, wtDoc("f00.txt").key()) != nil {
		t.Fatalf("len=%d, f00 still listed=%v", len(l), m.openFiles.find(m.currentWorktree, wtDoc("f00.txt").key()) != nil)
	}
	if m.statusMsg != "closed f00.txt (20 files open)" {
		t.Errorf("statusMsg = %q", m.statusMsg)
	}
}

// A commit version reopened in the preview is the same document: its place
// is kept and it is not loaded again (a commit's bytes never change).
func TestPreviewReusesTheOpenCommitVersion(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	loads := 0
	load := func(context.Context) ([]byte, error) { loads++; return []byte("x\n"), nil }
	src := fileSource{kind: srcCommit, rev: "abc"}
	m, cmd := m.openPreviewSrc(src, "x.txt", load)
	m = pumpAll(t, m, cmd)
	d := m.filesPreview
	d.p.cur = 0
	m, _ = m.openPreviewSrc(fileSource{kind: srcCommit, rev: "abc"}, "y.txt", load)
	m, cmd = m.openPreviewSrc(src, "x.txt", load)
	if cmd != nil || m.filesPreview != d || loads != 1 {
		t.Fatalf("reopen: cmd=%v same=%v loads=%d, want no load, the same doc", cmd != nil, m.filesPreview == d, loads)
	}
	if n := len(m.openFiles.list(m.currentWorktree)); n != 2 {
		t.Fatalf("entries = %d, want 2", n)
	}
}

// A document is in one frame at a time: shown full-screen, it leaves the
// preview.
func TestADocumentHasOneFrame(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	d := wtDoc("a.txt")
	m.filesPreview = d
	m = m.registerDoc(d)
	m, _ = m.openFileViewer("a.txt", 0)
	if m.filesPreview != nil || layerOf[*fileViewer](m).openFile != d {
		t.Fatalf("preview=%v viewer doc same=%v", m.filesPreview != nil, layerOf[*fileViewer](m).openFile == d)
	}
}

func TestOpenFilesArePerWorktree(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m, _ = m.openFileViewer("a.txt", 0)
	m = m.popLayer()
	home := m.currentWorktree
	m.currentWorktree = home + "-other"
	if len(m.openFiles.list(m.currentWorktree)) != 0 {
		t.Fatal("another worktree sees this worktree's files")
	}
	m.currentWorktree = home
	if len(m.openFiles.list(home)) != 1 {
		t.Fatal("the first worktree lost its list")
	}
}
