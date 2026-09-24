package tui

import (
	"fmt"
	"testing"
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
