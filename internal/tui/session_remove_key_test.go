package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// Serial: installs a process-global session manager.
//
// On the Worktrees tab, x on an exited session sub-row removes the session
// without opening a menu — the same removal the . menu's Remove session and
// the ctrl+\ popup's x perform. On a running session x refuses (kill first),
// and never falls through to [x] resolve.
func TestXRemovesAnExitedSessionRow(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 0.3`)
	m.focus, m.activeLeftTab = panelWorktrees, panelWorktrees
	m.sel[panelWorktrees] = 1 // the session sub-row under the current worktree
	if info, ok := m.selectedSession(); !ok || info.ID != s.Info().ID {
		t.Fatalf("precondition: session row not selected: %+v %v", info, ok)
	}

	// Running: x refuses and says why; the session stays.
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	if _, ok := domain.Sessions().Get(s.Info().ID); !ok {
		t.Fatal("x removed a RUNNING session")
	}
	if !strings.Contains(m.statusMsg, "kill") {
		t.Fatalf("x on a running session must say to kill it first, status = %q", m.statusMsg)
	}
	if strings.Contains(m.footerLine(), "[x] remove") {
		t.Fatalf("footer offers [x] remove on a running session: %q", m.footerLine())
	}

	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not exit")
	}
	m, _ = m.onSessionsChanged()
	if !strings.Contains(m.footerLine(), "[x] remove") {
		t.Fatalf("footer must advertise [x] remove on an exited session: %q", m.footerLine())
	}
	mm, _ = m.Update(keyMsg("x"))
	m = mm.(Model)
	if _, ok := domain.Sessions().Get(s.Info().ID); ok {
		t.Fatal("x did not remove the exited session")
	}
	if m.focus != panelWorktrees {
		t.Fatalf("focus left the Worktrees tab: %v", m.focus)
	}
}

// x on a worktree row (no session selected) keeps its meaning: nothing
// happens when there is no conflict to resolve, and no session is touched.
func TestXOnAWorktreeRowLeavesSessionsAlone(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 5`)
	m.focus, m.activeLeftTab = panelWorktrees, panelWorktrees
	m.sel[panelWorktrees] = 0
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	if _, ok := domain.Sessions().Get(s.Info().ID); !ok {
		t.Fatal("x on the worktree row removed a session")
	}
	if strings.Contains(m.statusMsg, "kill") {
		t.Fatalf("session refusal leaked onto a worktree row: %q", m.statusMsg)
	}
}
