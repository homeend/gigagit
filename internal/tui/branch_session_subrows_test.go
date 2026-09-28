package tui

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// branchSubRowIndex returns the Branches display index of the sub-row for
// session id, or -1. Scanned, never hard-coded: sort and filters move rows.
func branchSubRowIndex(m Model, id domain.SessionID) int {
	ents := m.branchEntries()
	for di, u := range m.displayIndices(panelBranches) {
		if u < len(ents) && ents[u].sess == id {
			return di
		}
	}
	return -1
}

// branchRowIndex returns the Branches display index of branch name, or -1.
func branchRowIndex(m Model, name string) int {
	ents := m.branchEntries()
	for di, u := range m.displayIndices(panelBranches) {
		if u < len(ents) && !ents[u].sub() && m.branches[ents[u].br].Name == name {
			return di
		}
	}
	return -1
}

// Serial: installs a process-global session manager.
//
// A session running in a branch's worktree shows as a sub-row directly under
// that branch on the Branches tab — the Worktrees tab's "  └ ● label  running"
// row, so the branch list says what is running where without switching tabs.
func TestBranchesTabShowsSessionSubRows(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 0.3`)
	m.focus, m.activeLeftTab = panelBranches, panelBranches

	bi := branchRowIndex(m, m.status.Branch)
	si := branchSubRowIndex(m, s.Info().ID)
	if bi < 0 || si != bi+1 {
		t.Fatalf("session sub-row at %d, want directly under branch %q at %d", si, m.status.Branch, bi)
	}
	rows, _ := m.panelView(panelBranches)
	if !strings.Contains(rows[si], "└ ●") || !strings.Contains(rows[si], s.Info().Label) {
		t.Fatalf("sub-row text = %q, want the Worktrees-style session row", rows[si])
	}

	// The sub-row is not a branch: every branch action refuses it, and the
	// panel's stable row key is the session's, not the parent branch's.
	m.sel[panelBranches] = si
	if _, ok := m.selectedBranch(); ok {
		t.Fatal("selectedBranch resolved a session sub-row")
	}
	if _, ok := m.backingIndex(panelBranches); ok {
		t.Fatal("backingIndex accepted a session sub-row")
	}
	if got := ids(availableActions(m)); got["start-agent"] || got["show-in-worktrees"] || got["branch-checkout"] || got["branch-delete"] {
		t.Fatalf("branch actions offered on a session sub-row: %v", got)
	}
	if k := m.rowKeyAt(panelBranches, si); k == m.status.Branch || !strings.Contains(k, string(s.Info().ID)) {
		t.Fatalf("sub-row key = %q, want one carrying the session id", k)
	}
	if info, ok := m.selectedSession(); !ok || info.ID != s.Info().ID {
		t.Fatalf("selectedSession on the Branches tab = %+v %v", info, ok)
	}
}

// Serial: installs a process-global session manager.
//
// Enter, x and the . menu on a Branches session sub-row do what they do on the
// Worktrees tab: open the console, remove an exited session (a running one is
// refused), offer Open / Kill or Remove. Enter never falls into the branch's
// own enter action.
func TestBranchSubRowKeysMirrorWorktrees(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 0.3`)
	m.focus, m.activeLeftTab = panelBranches, panelBranches
	m.sel[panelBranches] = branchSubRowIndex(m, s.Info().ID)
	if m.sel[panelBranches] < 0 {
		t.Fatal("precondition: no sub-row")
	}

	got := ids(availableActions(m))
	if !got["session-open"] || !got["session-kill"] || got["session-remove"] {
		t.Fatalf("running sub-row menu = %v, want Open + Kill", got)
	}

	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.console == nil || m.console.id != s.Info().ID {
		t.Fatalf("enter did not open the session's console: %+v", m.console)
	}
	m.console = nil
	m.focus = panelBranches

	mm, _ = m.Update(keyMsg("x"))
	m = mm.(Model)
	if _, ok := domain.Sessions().Get(s.Info().ID); !ok {
		t.Fatal("x removed a RUNNING session")
	}
	if !strings.Contains(m.statusMsg, "kill") {
		t.Fatalf("x on a running session must say to kill it first, status = %q", m.statusMsg)
	}

	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not exit")
	}
	m, _ = m.onSessionsChanged()
	if !strings.Contains(m.footerLine(), "[x] remove") {
		t.Fatalf("footer must advertise [x] remove on an exited sub-row: %q", m.footerLine())
	}
	if got := ids(availableActions(m)); !got["session-remove"] || got["session-kill"] {
		t.Fatalf("exited sub-row menu = %v, want Open + Remove", got)
	}
	mm, _ = m.Update(keyMsg("x"))
	m = mm.(Model)
	if _, ok := domain.Sessions().Get(s.Info().ID); ok {
		t.Fatal("x did not remove the exited session")
	}
	if m.focus != panelBranches {
		t.Fatalf("focus left the Branches tab: %v", m.focus)
	}
	if branchSubRowIndex(m, s.Info().ID) >= 0 {
		t.Fatal("removed session still has a sub-row")
	}
}

// startSessionIn starts a real (sleeping) session labelled name in dir on a
// fresh process-global manager shared by the test.
func startSessionIn(t *testing.T, m Model, dir, name string) domain.SessionID {
	t.Helper()
	s, err := m.svc.StartSession(t.Context(), config.ToolCommand{Category: "session", Name: name, Mode: "session", Command: "sleep 30"}, dir, "", 80, 20, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s.Info().ID
}

// Serial: installs a process-global session manager.
//
// A sub-row is glued to its parent under every sort mode and under the
// alt+digit slot filters: it inherits the branch's name and date, and it is
// visible exactly when the branch is.
func TestBranchSubRowsFollowParentUnderSortAndSlotFilter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	m := bfModel(t)
	// bfModel's HEAD (main) is checked out at /r, feat/wt at /r2 — point both
	// at real dirs and run one session in each. feat/wt sorts last by name
	// and by date (200d old), main first, so the orders differ and both must
	// keep the pairs.
	rMain, rWt := t.TempDir(), t.TempDir()
	m.worktrees = []model.Worktree{{Path: rMain, Branch: "main"}, {Path: rWt, Branch: "feat/wt"}}
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() {
		domain.Sessions().KillAll(t.Context())
		restore()
	})
	sMain := startSessionIn(t, m, rMain, "claude")
	sWt := startSessionIn(t, m, rWt, "sh")

	for _, mode := range []sortMode{sortDefault, sortNameAsc, sortNameDesc, sortDateAsc, sortDateDesc} {
		m.sortModes[panelBranches] = mode
		for _, tc := range []struct {
			branch string
			id     domain.SessionID
		}{{"main", sMain}, {"feat/wt", sWt}} {
			bi, si := branchRowIndex(m, tc.branch), branchSubRowIndex(m, tc.id)
			if bi < 0 || si != bi+1 {
				t.Errorf("sort %v: %s sub-row at %d, branch at %d", mode, tc.branch, si, bi)
			}
		}
	}
	m.sortModes[panelBranches] = sortDefault

	// Slot 3 shows only fix/*: main (HEAD) and feat/wt (checked out) are
	// exempt, so both stay with their sub-rows; feat/a and feat/old go.
	// The hidden verdicts are per BRANCH; with more rows than branches a
	// row-indexed lookup would hide the wrong rows.
	m.branchFilterSlot[panelBranches] = 3
	if got := strings.Join(names(m, panelBranches), ","); got != "main,main,fix/b,feat/wt,feat/wt" {
		t.Fatalf("slot 3 rows (sub-rows carry the parent's name) = %s", got)
	}
	// Slot 1 hides feat/* — but a checked-out branch is exempt, so feat/wt
	// and its sub-row stay.
	m.branchFilterSlot[panelBranches] = 1
	if got := strings.Join(names(m, panelBranches), ","); got != "main,main,fix/b,feat/wt,feat/wt" {
		t.Fatalf("slot 1 rows = %s", got)
	}
	m.branchFilterSlot[panelBranches] = 0

	// A / query matching only the session label keeps the branch AND its
	// sub-row (the Worktrees haystack rule); one matching only the branch
	// keeps both too.
	m.filterPanel, m.filterQuery = panelBranches, "claude"
	if got := strings.Join(names(m, panelBranches), ","); got != "main,main" {
		t.Fatalf("filter 'claude' rows = %s", got)
	}
	m.filterQuery = "feat/wt"
	if got := strings.Join(names(m, panelBranches), ","); got != "feat/wt,feat/wt" {
		t.Fatalf("filter 'feat/wt' rows = %s", got)
	}

	// The v1 Start agent / Open terminal rows stay on the branch row and
	// never appear on its sub-row.
	m.filterQuery = ""
	m.loading = false
	m.sel[panelBranches] = branchRowIndex(m, "feat/wt")
	if got := ids(availableActions(m)); !got["start-agent"] {
		t.Fatalf("branch row lost Start agent: %v", got)
	}
	m.sel[panelBranches] = branchSubRowIndex(m, sWt)
	if got := ids(availableActions(m)); got["start-agent"] || got["open-terminal"] || !got["session-open"] {
		t.Fatalf("sub-row menu = %v, want the session rows only", got)
	}
}

// Serial: installs a process-global session manager.
//
// X on a RUNNING session sub-row (Branches or Worktrees tab) kills the
// session and removes it from the list once it has exited — x only removes
// an exited one. The footer advertises it, the . menu carries the same
// action, and the console docked on that session closes with the row.
func TestCapitalXKillsAndRemovesRunningSession(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 30`)
	m.focus, m.activeLeftTab = panelBranches, panelBranches
	m.sel[panelBranches] = branchSubRowIndex(m, s.Info().ID)
	m, _ = m.onSessionsChanged()

	if !strings.Contains(m.footerLine(), "[X] kill+remove") {
		t.Fatalf("footer must advertise [X] kill+remove on a running sub-row: %q", m.footerLine())
	}
	if got := ids(availableActions(m)); !got["session-kill-remove"] {
		t.Fatalf("running sub-row menu = %v, want Kill and remove session", got)
	}
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.console == nil || m.console.id != s.Info().ID {
		t.Fatalf("enter did not open the session's console: %+v", m.console)
	}
	m.focus = panelBranches

	mm, _ = m.Update(keyMsg("X"))
	m = mm.(Model)
	if !strings.Contains(m.statusMsg, "killing") {
		t.Fatalf("X must announce the kill, status = %q", m.statusMsg)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("X did not kill the session")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := domain.Sessions().Get(s.Info().ID); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("X did not remove the killed session")
		}
		time.Sleep(10 * time.Millisecond)
	}
	m, _ = m.onSessionsChanged()
	if branchSubRowIndex(m, s.Info().ID) >= 0 {
		t.Fatal("killed session still has a sub-row")
	}
	if m.console != nil {
		t.Fatal("the removed session's console is still docked")
	}
	if m.focus != panelBranches {
		t.Fatalf("focus left the Branches tab: %v", m.focus)
	}
	if strings.Contains(m.footerLine(), "kill+remove") {
		t.Fatalf("footer still advertises X with no session row: %q", m.footerLine())
	}
}
