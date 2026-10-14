package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// manyFileStackModel is a real repo whose tip commit adds n files of 20 lines
// each — far more than the loader's window — with the commit's file tree open.
func manyFileStackModel(t *testing.T, n int) Model {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "c0")
	for i := range n {
		var b strings.Builder
		for j := range 20 {
			fmt.Fprintf(&b, "line %d of file %d\n", j, i)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "-m", "c1")

	m := New(domain.New(testRepo(t, dir)))
	m.width, m.height = 140, 40
	loaded, _ := m.Update(m.loadCmd()())
	mm := loaded.(Model)
	mm.sel[panelCommits] = 0
	mm, cmd := mm.openChangedFiles(mm.commits[0])
	if cmd != nil {
		u, _ := mm.Update(cmd())
		mm = u.(Model)
	}
	if mm.filesView == nil {
		t.Fatal("the commit's file tree did not open")
	}
	return tempPromptStore(t, mm)
}

// Scrolling a stack with the mouse wheel loads the files it brings near the
// viewport, exactly as the keys do. The wheel never pumped the queue, so a
// stack read by wheel stopped at "(not loaded yet)" for every file past the
// window the last key or arrival had loaded.
func TestStackWheelScrollLoadsFilesItReaches(t *testing.T) {
	t.Parallel()
	m := manyFileStackModel(t, 30)
	m = m.setStackedPref(true)
	var pick contentLine
	for _, l := range m.filesView.visible() {
		if l.path != "" {
			pick = l
			break
		}
	}
	u, cmd := m.openDiffForFileLine(pick)
	m = drainCmds(t, u.(Model), cmd)
	v := m.diffLayer()
	if v == nil || v.stk == nil {
		t.Fatal("the diff must have opened stacked")
	}
	last := len(v.stk.files) - 1
	if v.stk.files[last].load != stackIdle {
		t.Fatalf("precondition: the last file must still be idle after opening, it is %v", v.stk.files[last].load)
	}
	for range 400 {
		u, cmd := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
		m = drainCmds(t, u.(Model), cmd)
	}
	v = m.diffLayer()
	if f := v.stk.files[last]; f.load != stackLoaded || f.d == nil {
		t.Fatalf("wheeled to the bottom, the last file is still %v (not loaded)", f.load)
	}
}
