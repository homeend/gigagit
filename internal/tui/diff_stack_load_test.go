package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// stackRepoModel is a real repo whose tip commit touches three files, with the
// commit's file tree open in the files view — the state pressing enter on a
// file stacks from.
func stackRepoModel(t *testing.T) Model {
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
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-b", "main")
	write("a.go", "package a\n")
	write("b.go", "package b\n")
	write("c.go", "package c\n")
	run("add", ".")
	run("commit", "-m", "c0")
	write("a.go", "package a\n\nfunc A() {}\n")
	write("b.go", "package b\n\nfunc B() {}\n")
	write("c.go", "package c\n\nfunc C() {}\n")
	run("add", ".")
	run("commit", "-m", "c1")

	m := New(domain.New(testRepo(t, dir)))
	m.width, m.height = 140, 40
	loaded, _ := m.Update(m.loadCmd()())
	mm := loaded.(Model)
	if len(mm.commits) == 0 {
		t.Fatal("no commits loaded")
	}
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

// drainCmds runs a command and feeds every message it produces back through
// Update, to a fixed point — the test-side stand-in for the Bubble Tea loop.
// Batches nest (a stack's open batches its loads and its numstat read), so
// they are flattened rather than handed to Update as a message.
func drainCmds(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for round := 0; len(queue) > 0 && round < 200; round++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if msg == nil {
			continue
		}
		u, more := m.Update(msg)
		m = u.(Model)
		queue = append(queue, more)
	}
	return m
}

// End to end on a real repo: opening a file with the stacked preference on
// stacks the whole commit and lazily loads every file near the viewport,
// each with its own rows and counts, leaving nothing in flight.
func TestStackLoadsARealCommit(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
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
	if len(v.stk.files) != 3 {
		t.Fatalf("the commit touched 3 files, the stack has %d", len(v.stk.files))
	}
	if v.stk.inflight != 0 {
		t.Fatalf("%d loads left in flight", v.stk.inflight)
	}
	for i, f := range v.stk.files {
		if f.load != stackLoaded || f.d == nil || len(f.d.full) == 0 {
			t.Fatalf("file %d (%s) did not load: state %v", i, f.path, f.load)
		}
		if !f.counted || f.add == 0 {
			t.Fatalf("file %d (%s) has no counts: +%d −%d", i, f.path, f.add, f.del)
		}
	}
	if v.title != pick.path {
		t.Fatalf("the stack opened on %q, want the picked file %q", v.title, pick.path)
	}
	// Every file's rows are in ONE stream, under its own header.
	if got, want := len(v.blocks), 3; got != want {
		t.Fatalf("the stream has %d headers, want %d", got, want)
	}
}

// The real path: open a commit's file from the files view (single, the
// preference off), then press S — the same keys the headless capture drives.
func TestSStacksFromTheFilesViewPath(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	var pick contentLine
	for _, l := range m.filesView.visible() {
		if l.path != "" {
			pick = l
			break
		}
	}
	u, cmd := m.openDiffForFileLine(pick)
	m = drainCmds(t, u.(Model), cmd)
	if m.diffLayer() == nil || m.diffLayer().stk != nil {
		t.Fatal("with the preference off the file must open single")
	}
	u2, cmd2 := m.Update(keyMsg("S"))
	m = drainCmds(t, u2.(Model), cmd2)
	v := m.diffLayer()
	if v == nil || v.stk == nil {
		t.Fatalf("S must stack; notice %q status %q", m.diffNotice, m.statusMsg)
	}
	if len(v.stk.files) < 3 {
		t.Fatalf("the stack must hold the commit's files, got %d", len(v.stk.files))
	}
}
