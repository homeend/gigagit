package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
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
	// Clear row hides while the cursor sits ON the single mark ("Unmark
	// commit" covers it); move the cursor away and it appears.
	if _, ok := m.commitCompareClearRow(); ok {
		t.Fatal("clear row must hide when the cursor is on the only mark")
	}
	m.sel[panelCommits] = 1
	cr, ok := m.commitCompareClearRow()
	if !ok {
		t.Fatal("clear row must appear for an off-cursor mark")
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
	if !mm.inCompareMode() {
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

// TestCompareSetMarkerRenders proves the ◉ set marker actually paints on the
// Commits panel (the special graph-window render path), and that a set row does
// not eat the cursor indicator on the selected row.
func TestCompareSetMarkerRenders(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.width, m.height = 120, 40
	m.sel[panelCommits] = 0
	m.commitCompareSet = map[string]bool{m.commits[1].Hash: true} // mark row 1

	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx, -1)
	out := m.renderPanel(panelCommits, "Commits", rows, decos, 120, 12)

	if !strings.Contains(out, "◉") {
		t.Fatalf("set marker ◉ did not render on the Commits panel:\n%s", out)
	}
	if !strings.Contains(out, "> ") {
		t.Fatalf("cursor prefix lost — set membership must not eat the selection indicator:\n%s", out)
	}
}

// TestCompareSetMarkerBeatsMark proves a commit that is BOTH m-marked and in the
// compare set renders ◉ (set), never ◆ (mark) — they must stay distinguishable.
func TestCompareSetMarkerBeatsMark(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.width, m.height = 120, 40
	m.sel[panelCommits] = 2 // cursor elsewhere so "> " isn't on the test row
	m.mark = &markState{panel: panelCommits, key: m.commits[0].Hash, display: m.commits[0].Hash}
	m.commitCompareSet = map[string]bool{m.commits[0].Hash: true}

	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx, -1)
	out := m.renderPanel(panelCommits, "Commits", rows, decos, 120, 12)

	if !strings.Contains(out, "◉") {
		t.Fatalf("set marker ◉ missing:\n%s", out)
	}
	if strings.Contains(out, "◆") {
		t.Fatalf("mark glyph ◆ rendered for a set member — ◉ must take precedence:\n%s", out)
	}
}

func TestShiftDownGrowsCompareSelection(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	u, _ := m.Update(keyMsg("shift+down"))
	mm := u.(Model)
	// Both the start row and the landed row are in the set.
	if !mm.commitCompareSet[m.commits[0].Hash] || !mm.commitCompareSet[m.commits[1].Hash] {
		t.Fatalf("shift+down must add the start and the landed commit: %v", mm.commitCompareSet)
	}
	if mm.sel[panelCommits] != 1 {
		t.Fatalf("cursor must move to row 1, got %d", mm.sel[panelCommits])
	}
}

// TestUnmarkRowsVisibility pins the three .-menu unmark states: cursor-on-mark
// → "Unmark commit"; ≥2 marks → "Unmark all commits (N)"; exactly one mark
// with the cursor elsewhere → "Unmark the marked commit".
func TestUnmarkRowsVisibility(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits

	// Cursor on a marked commit → toggle row reads "Unmark commit".
	m.commitCompareSet = map[string]bool{m.commits[0].Hash: true}
	m.sel[panelCommits] = 0
	r, ok := m.commitCompareToggleRow()
	if !ok || r.label != "Unmark commit" {
		t.Fatalf("cursor-on-mark toggle row = %q ok=%v, want \"Unmark commit\"", r.label, ok)
	}
	// ...and the clear row is hidden (the toggle row covers this state).
	if _, ok := m.commitCompareClearRow(); ok {
		t.Fatal("clear row must hide when the single mark is under the cursor")
	}

	// Cursor elsewhere with the single mark → "Unmark the marked commit".
	m.sel[panelCommits] = 1
	cr, ok := m.commitCompareClearRow()
	if !ok || cr.label != "Unmark the marked commit" {
		t.Fatalf("single off-cursor mark row = %q ok=%v", cr.label, ok)
	}

	// Two marks → "Unmark all commits (2)" regardless of the cursor.
	m.commitCompareSet[m.commits[1].Hash] = true
	cr, ok = m.commitCompareClearRow()
	if !ok || cr.label != "Unmark all commits (2)" {
		t.Fatalf("two-mark row = %q ok=%v, want \"Unmark all commits (2)\"", cr.label, ok)
	}
	mm, _ := cr.run(m)
	if n := len(mm.(Model).commitCompareSet); n != 0 {
		t.Fatalf("running unmark-all must empty the set, got %d", n)
	}

	// Unmarked cursor row advertises the space gesture.
	m.commitCompareSet = nil
	m.sel[panelCommits] = 0
	r, ok = m.commitCompareToggleRow()
	if !ok || r.label != "Add to compare selection (space)" {
		t.Fatalf("unmarked toggle row = %q ok=%v", r.label, ok)
	}
}
