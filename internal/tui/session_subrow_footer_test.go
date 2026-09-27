package tui

import (
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// Serial: installs a process-global session manager.
//
// A session sub-row advertises what enter does there — [enter] open — on both
// tabs that list sub-rows, and the parent row keeps its own enter hint
// ([enter] switch on a worktree, [enter] tip on a branch) instead.
func TestSessionSubRowFooterOffersEnterOpen(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	s := startTestSession(t, m, `sleep 0.3`)
	for _, p := range []panel{panelWorktrees, panelBranches} {
		m.focus, m.activeLeftTab = p, p
		var parent, sub int
		if p == panelWorktrees {
			parent, sub = 0, 1
		} else {
			parent, sub = branchRowIndex(m, m.status.Branch), branchSubRowIndex(m, s.Info().ID)
		}
		m.sel[p] = sub
		if info, ok := m.selectedSession(); !ok || info.ID != s.Info().ID {
			t.Fatalf("%v: precondition, sub-row not selected", p)
		}
		if f := m.footerLine(); !strings.Contains(f, "[enter] open") || strings.Contains(f, "[enter] switch") || strings.Contains(f, "[enter] tip") {
			t.Errorf("%v sub-row footer = %q, want [enter] open and no parent enter hint", p, f)
		}
		m.sel[p] = parent
		if f := m.footerLine(); strings.Contains(f, "[enter] open") {
			t.Errorf("%v parent row footer offers [enter] open: %q", p, f)
		}
	}
}

// Serial: installs a process-global session manager.
//
// The Worktrees / filter treats a worktree and its sub-rows as one unit, as
// the Branches tab does: a query naming only a session's label keeps the
// worktree it runs in (and the sub-row), a query naming only the worktree
// keeps its sub-rows.
func TestWorktreesFilterMatchesWorktreeWithItsSubRows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	m := bfModel(t)
	rMain, rWt := t.TempDir(), t.TempDir()
	m.worktrees = []model.Worktree{{Path: rMain, Branch: "main"}, {Path: rWt, Branch: "feat/wt"}}
	m.focus, m.activeLeftTab = panelWorktrees, panelWorktrees
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() {
		domain.Sessions().KillAll(t.Context())
		restore()
	})
	startSessionIn(t, m, rWt, "claude")

	wtNames := func() string {
		_, idx := m.panelView(panelWorktrees)
		l := m.listFor(panelWorktrees)
		out := make([]string, len(idx))
		for i, b := range idx {
			out[i] = l.Name(b)
		}
		return strings.Join(out, ",")
	}
	m.filterPanel, m.filterQuery = panelWorktrees, "claude"
	if got := wtNames(); got != "feat/wt,feat/wt" {
		t.Fatalf("filter 'claude' rows = %s, want the worktree and its sub-row", got)
	}
	m.filterQuery = "feat/wt"
	if got := wtNames(); got != "feat/wt,feat/wt" {
		t.Fatalf("filter 'feat/wt' rows = %s", got)
	}
	m.filterQuery = "main"
	if got := wtNames(); got != "main" {
		t.Fatalf("filter 'main' rows = %s", got)
	}
}
