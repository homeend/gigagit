package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// loadedModelLinearCommits builds a real repo with n linear commits (commit k
// adds fileK.txt) and returns a loaded model. m.commits is newest-first, so
// m.commits[0] is the tip and m.commits[n-1] is the root.
func loadedModelLinearCommits(t *testing.T, n int) Model {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
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
	for k := 0; k < n; k++ {
		os.WriteFile(filepath.Join(dir, "file"+strconv.Itoa(k)+".txt"), []byte("v\n"), 0o644)
		run("add", ".")
		run("commit", "-m", "c"+strconv.Itoa(k))
	}
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	mm := loaded.(Model)
	if len(mm.commits) < n {
		t.Fatalf("expected ≥%d commits, got %d", n, len(mm.commits))
	}
	return mm
}

func TestCompareSetToggleAndClear(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	// No set → toggle row labeled "Add", clear row absent.
	r, ok := m.commitCompareToggleRow()
	if !ok {
		t.Fatal("toggle row must be present on a commit")
	}
	if _, ok := m.commitCompareClearRow(); ok {
		t.Fatal("clear row must be absent with an empty set")
	}
	mm, _ := r.run(m)
	m = mm.(Model) // add commit[0]
	if !m.commitCompareSet[m.commits[0].Hash] {
		t.Fatal("commit[0] must be in the set after add")
	}
	// Display indices include the member.
	if !m.compareSetDisplayIndices(panelCommits)[0] {
		t.Fatal("display index 0 must be marked")
	}
	// Clear row now present; clearing empties the set.
	cr, ok := m.commitCompareClearRow()
	if !ok {
		t.Fatal("clear row must appear with a non-empty set")
	}
	mm, _ = cr.run(m)
	m = mm.(Model)
	if len(m.commitCompareSet) != 0 {
		t.Fatalf("set must be empty after clear, got %d", len(m.commitCompareSet))
	}
}

func TestCompareSelectionTwoCommits(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{
		m.commits[0].Hash: true, // newer
		m.commits[1].Hash: true, // older
	}
	left, right, note, ok := m.compareSelectionEndpoints()
	if !ok {
		t.Fatalf("two commits must be comparable: %q", note)
	}
	// 2 commits → tree-diff older↔newer, no ^.
	if left.Hash != m.commits[1].Hash || right.Hash != m.commits[0].Hash {
		t.Fatalf("endpoints = %s ↔ %s, want %s ↔ %s", left.Hash, right.Hash, m.commits[1].Hash, m.commits[0].Hash)
	}
}

func TestCompareSelectionThreeCommitsSquash(t *testing.T) {
	m := loadedModelLinearCommits(t, 4) // c0(root)..c3(tip); select the top 3 (oldest selected = c1, has a parent)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{
		m.commits[0].Hash: true,
		m.commits[1].Hash: true,
		m.commits[2].Hash: true, // oldest selected; its parent is c0
	}
	left, right, note, ok := m.compareSelectionEndpoints()
	if !ok {
		t.Fatalf("three non-root commits must squash: %q", note)
	}
	if left.Hash != m.commits[2].Hash+"^" {
		t.Fatalf("squash base = %q, want %q", left.Hash, m.commits[2].Hash+"^")
	}
	if right.Hash != m.commits[0].Hash {
		t.Fatalf("squash tip = %q, want %q", right.Hash, m.commits[0].Hash)
	}
}

func TestCompareSelectionRootSquashRefused(t *testing.T) {
	m := loadedModelLinearCommits(t, 3) // select all 3 → oldest selected is the root
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{
		m.commits[0].Hash: true,
		m.commits[1].Hash: true,
		m.commits[2].Hash: true, // root
	}
	_, _, note, ok := m.compareSelectionEndpoints()
	if ok {
		t.Fatal("squashing from the root commit must be refused")
	}
	if note == "" {
		t.Fatal("refusal must carry a status note")
	}
}

func TestCompareSelectionRowRunsRealSquashDiff(t *testing.T) {
	m := loadedModelLinearCommits(t, 4)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{
		m.commits[0].Hash: true,
		m.commits[1].Hash: true,
		m.commits[2].Hash: true,
	}
	r, ok := m.commitCompareSelectionRow()
	if !ok {
		t.Fatal("Compare selection row must be present with 3 in the set")
	}
	u, cmd := r.run(m)
	mm := u.(Model)
	if !mm.filesCompare {
		t.Fatal("running must open compare mode")
	}
	// Drive the file list: the squash (c1^..c3 = c1,c2,c3) added file1/2/3.
	cm, ok := cmd().(compareFilesMsg)
	if !ok || cm.err != nil {
		t.Fatalf("compareFilesMsg=%v err=%v", ok, cm.err)
	}
	u, _ = mm.Update(cm)
	mm = u.(Model)
	var pick contentLine
	for _, l := range mm.filesView.lines {
		if l.path == "file2.txt" && l.status == "A" {
			pick = l
		}
	}
	if pick.path == "" {
		t.Fatalf("file2.txt (A) missing from squash list: %+v", mm.filesView.lines)
	}
	// Drive that file's diff: real rows through the cached commit↔commit key.
	sel := -1
	for i, l := range mm.filesView.lines {
		if l.path == "file2.txt" {
			sel = i
		}
	}
	mm.filesView.sel = sel
	mm.filesTreeFocused = true
	u, dcmd := mm.Update(keyMsg("enter"))
	mm = u.(Model)
	dmsg := dcmd().(diffMsg)
	if dmsg.view.err != nil || len(dmsg.view.full) == 0 {
		t.Fatalf("squash file diff: err=%v rows=%d", dmsg.view.err, len(dmsg.view.full))
	}
}

func TestCompareSelectionRowAbsentUnderTwo(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{m.commits[0].Hash: true}
	if _, ok := m.commitCompareSelectionRow(); ok {
		t.Fatal("Compare selection row must be absent with fewer than 2 selected")
	}
}
