package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

func markModel() Model {
	return Model{
		branches:  []model.Branch{{Name: "main", IsHead: true}, {Name: "feat/a"}, {Name: "feat/b"}},
		commits:   []model.Commit{{Hash: "1111111", Subject: "one"}, {Hash: "2222222", Subject: "two"}},
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		focus:     panelBranches,
	}
}

func pressRune(t *testing.T, m Model, r string) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)})
	return updated.(Model)
}

func pressType(t *testing.T, m Model, kt tea.KeyType) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: kt})
	return updated.(Model)
}

func TestMarkToggle(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m")
	if m.mark == nil || m.mark.key != "main" || m.mark.panel != panelBranches {
		t.Fatalf("mark = %+v, want main on branches", m.mark)
	}
	m = pressRune(t, m, "m") // same row again: unmark
	if m.mark != nil {
		t.Fatal("second m on the marked row must unmark")
	}
}

func TestMarkMovesAcrossPanels(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m") // mark main on branches
	m.focus = panelCommits
	m = pressRune(t, m, "m")
	if m.mark == nil || m.mark.panel != panelCommits || m.mark.key != "1111111" {
		t.Fatalf("mark = %+v, want commit 1111111", m.mark)
	}
	if m.pairPopup != nil {
		t.Fatal("cross-panel m must move the mark, not open the popup")
	}
}

func TestMarkPairOpensPopupOnBranches(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m") // mark main
	m.sel[panelBranches] = 1 // feat/a
	m = pressRune(t, m, "m")
	if m.pairPopup == nil {
		t.Fatal("expected the pair-op popup")
	}
	if m.pairPopup.marked != "main" || m.pairPopup.selected != "feat/a" {
		t.Fatalf("popup pair = %s + %s", m.pairPopup.marked, m.pairPopup.selected)
	}
	if len(m.pairPopup.ops) != 2 {
		t.Fatalf("branches must register merge + rebase, got %d ops", len(m.pairPopup.ops))
	}
}

func TestMarkPairNoOpsPanel(t *testing.T) {
	m := markModel()
	m.focus = panelCommits
	m = pressRune(t, m, "m")
	m.sel[panelCommits] = 1
	m = pressRune(t, m, "m")
	if m.pairPopup != nil {
		t.Fatal("commits panel has no pair ops")
	}
	if !strings.Contains(m.statusMsg, "no pair operations") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestMarkSurvivesResortByIdentity(t *testing.T) {
	m := markModel()
	m.sel[panelBranches] = 1 // feat/a
	m = pressRune(t, m, "m")
	m.sortModes[panelBranches] = sortNameDesc // main, feat/b, feat/a
	if got := m.markDisplayIndex(panelBranches); got != 2 {
		t.Fatalf("markDisplayIndex = %d after resort, want 2 (identity, not index)", got)
	}
}

func TestDeadMarkRemarksInsteadOfPairing(t *testing.T) {
	m := markModel()
	m.sel[panelBranches] = 2 // feat/b
	m = pressRune(t, m, "m")
	// feat/b disappears (e.g. deleted elsewhere + reload)
	m.branches = []model.Branch{{Name: "main", IsHead: true}, {Name: "feat/a"}}
	m.sel[panelBranches] = 0
	m = pressRune(t, m, "m")
	if m.pairPopup != nil {
		t.Fatal("a dead mark must not open the popup")
	}
	if m.mark == nil || m.mark.key != "main" {
		t.Fatalf("mark = %+v, want re-marked main", m.mark)
	}
}

func TestEscClearsMarkBeforeFilter(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m")
	m.filterPanel = panelBranches
	m.filterQuery = "fe"
	m = pressType(t, m, tea.KeyEsc)
	if m.mark != nil {
		t.Fatal("first esc must clear the mark")
	}
	if m.filterQuery != "fe" {
		t.Fatal("first esc must NOT clear the filter while a mark exists")
	}
	m = pressType(t, m, tea.KeyEsc)
	if m.filterQuery != "" {
		t.Fatal("second esc must clear the filter")
	}
}

// The Rebase pair-op reads marked-first, matching Merge: the MARKED branch is
// rebased ONTO the SELECTED branch (mark XXX, select YYY → "Rebase XXX onto YYY").
func TestRebasePairOpDirection(t *testing.T) {
	ops := pairOpsFor(panelBranches)
	var rebase *pairOp
	for i := range ops {
		if strings.HasPrefix(ops[i].label("main", "feat"), "Rebase ") {
			rebase = &ops[i]
		}
	}
	if rebase == nil || !rebase.enabled || rebase.build == nil {
		t.Fatal("Rebase pair-op must be enabled with a build func")
	}
	if got := rebase.label("main", "feat"); got != "Rebase main onto feat" {
		t.Fatalf("label = %q, want %q", got, "Rebase main onto feat")
	}
	op, ok := rebase.build("main", "feat").(engine.SmartRebase) // marked=main, selected=feat
	if !ok {
		t.Fatalf("build returned %T, want engine.SmartRebase", rebase.build("main", "feat"))
	}
	if op.Branch != "main" || op.Onto != "feat" {
		t.Fatalf("SmartRebase = %+v, want {Branch:main Onto:feat}", op)
	}
}

// Integration: enter on Rebase dispatches SmartRebase and the rebase really
// runs (feat replayed onto main; we end on feat).
func TestPairPopupEnterRunsSmartRebase(t *testing.T) {
	dir, repo := newRepoDir(t)
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
	// feat diverges from main with a disjoint file; main advances disjointly.
	run("checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "feat change")
	run("checkout", "main")
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("m\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "main change")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelBranches

	_, idx := m.panelView(panelBranches)
	l := m.listFor(panelBranches)
	for n, i := range idx {
		if l.Key(i) == "feat" {
			m.sel[panelBranches] = n
		}
	}
	m = pressRune(t, m, "m") // mark feat (the branch to rebase)
	for n, i := range idx {
		if l.Key(i) == "main" {
			m.sel[panelBranches] = n
		}
	}
	m = pressRune(t, m, "m") // popup: Merge is entry 0, Rebase is entry 1
	if m.pairPopup == nil {
		t.Fatal("popup expected")
	}
	m = pressRune(t, m, "j") // move to the Rebase entry
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.mark != nil || m.pairPopup != nil {
		t.Fatal("enter must clear both the popup and the mark")
	}
	m = driveOp(t, m, cmd)
	// feat was rebased onto main → main's commit is now in feat's tree, and we
	// end on feat (rung 3 switches to it).
	if _, err := os.Stat(filepath.Join(dir, "main.txt")); err != nil {
		t.Fatal("rebase did not run: main.txt missing (feat not replayed onto main)")
	}
}

func TestPairPopupEscKeepsMark(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m")
	m.sel[panelBranches] = 1
	m = pressRune(t, m, "m")
	m = pressType(t, m, tea.KeyEsc)
	if m.pairPopup != nil {
		t.Fatal("esc must close the popup")
	}
	if m.mark == nil {
		t.Fatal("esc must keep the mark (user may pick another row)")
	}
}

func TestMarkedRowRendersDiamondAndStatusHint(t *testing.T) {
	m := markModel()
	m.width, m.height = 80, 24
	m.sel[panelBranches] = 1
	m = pressRune(t, m, "m")
	out := m.render()
	if !strings.Contains(out, "◆") {
		t.Fatal("marked row must render the ◆ prefix")
	}
	if !strings.Contains(out, "marked: feat/a") {
		t.Fatal("status line must show the mark hint")
	}
}

func TestReRootClearsMark(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m")
	if m.mark == nil {
		t.Fatal("setup: mark expected")
	}
	updated, _ := m.reRoot(t.TempDir())
	if got := updated.(Model).mark; got != nil {
		t.Fatalf("mark = %+v after reRoot, want nil", got)
	}
}

// Integration: enter on Merge dispatches SmartMerge and the merge really runs.
func TestPairPopupEnterRunsSmartMerge(t *testing.T) {
	dir, repo := newRepoDir(t)
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
	run("checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "feat change")
	run("checkout", "main")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelBranches

	// find and mark feat, then select main and pair
	_, idx := m.panelView(panelBranches)
	l := m.listFor(panelBranches)
	for n, i := range idx {
		if l.Key(i) == "feat" {
			m.sel[panelBranches] = n
		}
	}
	m = pressRune(t, m, "m") // mark feat
	for n, i := range idx {
		if l.Key(i) == "main" {
			m.sel[panelBranches] = n
		}
	}
	m = pressRune(t, m, "m") // popup: Merge feat into main is the first entry
	if m.pairPopup == nil {
		t.Fatal("popup expected")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.mark != nil || m.pairPopup != nil {
		t.Fatal("enter must clear both the popup and the mark")
	}
	m = driveOp(t, m, cmd)
	if _, err := os.Stat(filepath.Join(dir, "feat.txt")); err != nil {
		t.Fatal("merge did not run: feat.txt missing on main")
	}
}
