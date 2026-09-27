package tui

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/domain"
)

// The Branches `.` menu offers Start agent… / Open terminal on exactly the
// branches that are checked out in some worktree — the ones whose row carries
// the worktree path — because only those exist on disk to run anything in.
func TestBranchSessionRowsOfferedOnCheckedOutBranches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		sel  int
		want bool
		wt   string
	}{
		{0, true, "feature"}, // feature: a linked worktree
		{1, true, "repo"},    // main: the current worktree counts too
		{2, false, ""},       // loose: no worktree, nothing on disk
	} {
		m := showInWorktreesModel()
		m.sel[panelBranches] = tc.sel
		rows := availableActions(m)
		got := ids(rows)
		name := m.branches[tc.sel].Name
		if got["start-agent"] != tc.want || got["open-terminal"] != tc.want {
			t.Errorf("branch %q: start-agent=%v open-terminal=%v, want both %v", name, got["start-agent"], got["open-terminal"], tc.want)
			continue
		}
		if !tc.want {
			continue
		}
		// The rows say where they land — the worktree is not otherwise
		// visible from the Branches tab.
		for _, id := range []string{"start-agent", "open-terminal"} {
			r, _ := rowByID(rows, id)
			if !strings.Contains(r.label, tc.wt) {
				t.Errorf("branch %q: %s label %q does not name worktree %q", name, id, r.label, tc.wt)
			}
		}
	}
}

func TestBranchSessionRowsAbsentOffBranchesTab(t *testing.T) {
	t.Parallel()
	for _, p := range []panel{panelRemotes, panelWorktrees} {
		m := showInWorktreesModel()
		m.focus, m.activeLeftTab = p, p
		rows := availableActions(m)
		// The Worktrees tab keeps its own (unqualified) rows; the Remotes tab
		// has none. Either way the Branches rows must not leak.
		for _, r := range rows {
			if (r.id == "start-agent" || r.id == "open-terminal") && strings.Contains(r.label, " in ") {
				t.Errorf("focus %v: Branches-tab row %q leaked", p, r.label)
			}
		}
		if p == panelRemotes && (ids(rows)["start-agent"] || ids(rows)["open-terminal"]) {
			t.Errorf("Remotes tab offers agent/terminal rows")
		}
	}
}

// Serial: installs a process-global session manager.
func TestBranchOpenTerminalRowStartsInThatWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	dir, _ := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-feature")
	runGit(t, dir, "worktree", "add", "-b", "feature/a", wt, "main")
	m := loadedModelAt(t, dir)
	m.width, m.height = 120, 40
	m.cfg.Console.Shell = "/bin/sh"
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(t.Context()); restore() })

	m.focus, m.activeLeftTab = panelBranches, panelBranches
	sel := -1
	for i, b := range m.branches {
		if b.Name == "feature/a" {
			sel = i
		}
	}
	if sel < 0 {
		t.Fatalf("feature/a not loaded: %+v", m.branches)
	}
	m.sel[panelBranches] = sel
	row, ok := rowByID(availableActions(m), "open-terminal")
	if !ok {
		t.Fatal("no Open terminal row on a checked-out branch")
	}
	mm, cmd := row.run(m)
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("Open terminal returned no command")
	}
	mm, _ = m.Update(cmd())
	m = mm.(Model)
	if m.console == nil || !m.console.focused {
		t.Fatalf("the terminal console must open focused: %+v", m.console)
	}
	s, ok := m.consoleSession()
	if !ok || s.Info().Label != "Terminal" {
		t.Fatalf("session = %+v", s)
	}
	if got := s.Info().Dir; filepath.Clean(got) != filepath.Clean(wt) {
		t.Fatalf("terminal started in %q, want the branch's worktree %q", got, wt)
	}
}
