package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// symModel is mergePreviewModel (main, feat/x, the saved "login" preview)
// plus feat/y off main adding y.txt, with the checkout set so links build.
func symModel(t *testing.T) Model {
	t.Helper()
	m, dir, _ := mergePreviewModel(t)
	runGit(t, dir, "checkout", "-q", "-b", "feat/y", "main")
	if err := os.WriteFile(filepath.Join(dir, "y.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "add y")
	runGit(t, dir, "checkout", "-q", "main")
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)
	m.width, m.height = 160, 40
	if len(m.worktrees) == 0 {
		t.Fatal("fixture: no worktree to address links by")
	}
	m.currentWorktree = m.worktrees[0].Path
	return m
}

func TestSymmetricPairRowOpensAnEmptyBasePopup(t *testing.T) {
	t.Parallel()
	m := symModel(t)
	m, _ = m.openSymmetricBase("feat/x", "feat/y")
	p, ok := m.topLayer().(*symmetricBasePopup)
	if !ok {
		t.Fatalf("top layer = %T, want the base popup", m.topLayer())
	}
	if p.base.Value() != "" {
		t.Fatalf("the base must start EMPTY, got %q", p.base.Value())
	}
	// Enter on an empty base does nothing: there is no default.
	m, cmd := send(m, keyType(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("enter with no base must not save")
	}
	if _, ok := m.topLayer().(*symmetricBasePopup); !ok {
		t.Fatal("the popup must stay open")
	}
}

func TestSymmetricBaseRefusesAOrB(t *testing.T) {
	t.Parallel()
	m := symModel(t)
	m, _ = m.openSymmetricBase("feat/x", "feat/y")
	m = typeText(t, m, "feat/x")
	m, cmd := send(m, keyType(tea.KeyEnter))
	if cmd != nil || !strings.Contains(m.statusMsg, "base") {
		t.Fatalf("base == A must be refused in place: cmd=%v status=%q", cmd != nil, m.statusMsg)
	}
	if _, ok := m.topLayer().(*symmetricBasePopup); !ok {
		t.Fatal("the popup must stay open after a refusal")
	}
}

func TestSymmetricSaveOpensTheComparisonWithLoading(t *testing.T) {
	t.Parallel()
	m := symModel(t)
	m, _ = m.openSymmetricBase("feat/x", "feat/y")
	m = typeText(t, m, "main")
	m, cmd := send(m, keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter with a base must save")
	}
	m, cmd = send(m, cmd()) // symmetricSavedMsg
	if _, ok := m.topLayer().(*compareLoadingPopup); !ok {
		t.Fatalf("the save must open the comparison behind the loading popup, top = %T (status %q)", m.topLayer(), m.statusMsg)
	}
	m = pumpAll(t, m, cmd)
	if m.filesSets == nil || !strings.HasSuffix(m.filesSets.LeftText, "@main...feat/x") || !strings.HasSuffix(m.filesSets.RightText, "@main...feat/y") {
		t.Fatalf("comparison did not open: %+v (status %q)", m.filesSets, m.statusMsg)
	}
	cs, _ := m.svc.SavedCompareList(context.Background())
	if len(cs) != 2 { // the fixture's merge preview + the symmetric entry
		t.Fatalf("stored = %+v", cs)
	}
}

func TestSymmetricBaseEscReturnsToThePairMenu(t *testing.T) {
	t.Parallel()
	m := symModel(t)
	ops := pairOpsFor(panelBranches)
	p := newPairOpPopup(m.width, "feat/x", "feat/y", ops)
	p.sel = len(ops) - 1 // the symmetric row is last
	if !strings.Contains(ops[p.sel].label("feat/x", "feat/y"), "Symmetric") {
		t.Fatalf("last pair row = %q", ops[p.sel].label("feat/x", "feat/y"))
	}
	m = m.pushLayer(p)
	m, _ = send(m, keyType(tea.KeyEnter))
	if _, ok := m.topLayer().(*symmetricBasePopup); !ok {
		t.Fatalf("top = %T, want the base popup", m.topLayer())
	}
	m, _ = send(m, keyType(tea.KeyEsc))
	if _, ok := m.topLayer().(*pairOpPopup); !ok {
		t.Fatalf("esc must return to the pair menu, top = %T", m.topLayer())
	}
}
