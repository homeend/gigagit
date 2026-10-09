package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// NOT parallel: these flip the platform's fold rule for their duration;
// top-level parallel tests only resume once every sequential one is done.
func withCaseFold(t *testing.T) {
	t.Helper()
	was := model.CaseInsensitivePaths()
	model.SetCaseInsensitivePaths(true)
	t.Cleanup(func() { model.SetCaseInsensitivePaths(was) })
}

// On a case-folding platform a session, a link or the user's cwd can spell
// a listed worktree in another case: the slots meet on the checkout key,
// and the listed spelling is the one gg uses on disk.
func TestSlotsMeetOnTheCheckoutKey(t *testing.T) {
	withCaseFold(t)
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	upper := strings.ToUpper(other) // not on disk as spelled: only the key says it is the listed one
	if !m.isRepoWorktree(upper) {
		t.Fatalf("%s is the listed %s spelled otherwise: must count as this repo's", upper, other)
	}
	if m.ensureView(upper) != m.ensureView(other) {
		t.Fatal("two slots for one directory")
	}
	m, ok := m.switchView(upper)
	if !ok {
		t.Fatalf("switchView(%s) refused: %s", upper, m.statusMsg)
	}
	if m.viewed != model.KeyOf(other) || m.currentWorktree != filepath.Clean(other) {
		t.Fatalf("viewed=%q currentWorktree=%q: want the listed spelling on disk", m.viewed, m.currentWorktree)
	}
}

// The self-guard holds when home was recorded in another spelling than
// the list's (the user started gg from a differently-cased cwd).
func TestHomeGuardHoldsAcrossSpellings(t *testing.T) {
	withCaseFold(t)
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m.home, m.viewed = "", ""
	m = m.seedHome(strings.ToUpper(m.currentWorktree))
	m.focus, m.activeLeftTab = panelWorktrees, panelWorktrees
	m.sel[panelWorktrees] = 0
	wt, ok := m.selectedWorktree()
	if !ok || !model.SamePath(wt.Path, m.currentWorktree) {
		t.Fatalf("row 0 = %+v ok=%v, want home", wt, ok)
	}
	if m.canDeleteWorktree() {
		t.Fatal("offered to delete home because its key was spelled otherwise")
	}
}
