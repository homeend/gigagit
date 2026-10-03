package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// A console shows only a session of the repository on screen: one that runs
// in one of its worktrees (the Worktrees sub-rows' rule). Another
// repository's sessions are reached through the ctrl+\ popup, which switches
// to their repository first.

// consoleSwitch is what a repo switch leaves for its snapshot to settle.
type consoleSwitch struct {
	armed bool             // reRoot ran; the console has not been checked against the new repo yet
	open  domain.SessionID // the session the switch was asked for, opened once the repo has loaded
	tour  string           // an agent tour (overview id) to show once the worktree has loaded
}

// inRepo reports whether a session runs in one of this repository's worktrees.
func (m Model) inRepo(info domain.SessionInfo) bool {
	dir := filepath.Clean(info.Dir)
	for _, w := range m.worktrees {
		if filepath.Clean(w.Path) == dir {
			return true
		}
	}
	return false
}

// repoSessions keeps the sessions of this repository.
func (m Model) repoSessions(list []domain.SessionInfo) []domain.SessionInfo {
	var out []domain.SessionInfo
	for _, info := range list {
		if m.inRepo(info) {
			out = append(out, info)
		}
	}
	return out
}

// hasRepoSessions reports a running session of one kind (terminals or agents)
// in this repository — what alt+a / alt+t cycle.
func (m Model) hasRepoSessions(terminal bool) bool {
	return len(sessionsByLastUsed(m.repoSessions(domain.Sessions().List()), terminal)) > 0
}

// openSessionAnywhere opens a session's console; one of another repository's
// sessions switches to that repository first, and its console opens once the
// switch has loaded. ctrl+\ opens over anything, so — like a switch asked
// from the hosted web page — the switch is refused while what is still open
// owns the screen (steerRefusal).
func (m Model) openSessionAnywhere(id domain.SessionID) (Model, tea.Cmd) {
	s, ok := domain.Sessions().Get(id)
	if !ok || m.inRepo(s.Info()) {
		return m.openConsole(id) // a gone session says so there
	}
	if m.steerRefusal() != "" {
		m.statusMsg = i18n.T("that session runs in another repository — close what is open first, then open it again")
		return m, nil
	}
	dir := s.Info().Dir
	if verdict, _ := checkSwitchTarget(guardStat, guardGOOS, dir); verdict != switchOK {
		m.statusMsg = i18n.T("cannot switch: %s is not reachable from here", dir)
		return m, nil
	}
	nm, cmd := m.reRoot(dir)
	m = nm.(Model)
	m.consoleSwitch.open = id
	return m, cmd
}

// settleConsoleAfterSwitch runs when a switch's snapshot has landed (so
// m.worktrees lists the new repository's): the session the switch was asked
// for opens; otherwise a console showing a session the new repository does
// not own closes. The session itself keeps running.
func (m Model) settleConsoleAfterSwitch() (Model, tea.Cmd) {
	tour := m.consoleSwitch.tour
	m, cmd := m.settleConsole()
	if tour != "" {
		m = m.showTour(tour) // the snapshot synced the worktree's overviews first
	}
	return m, cmd
}

func (m Model) settleConsole() (Model, tea.Cmd) {
	if !m.consoleSwitch.armed {
		return m, nil
	}
	open := m.consoleSwitch.open
	m.consoleSwitch = consoleSwitch{}
	if open != "" {
		return m.openConsole(open)
	}
	if m.console == nil {
		return m, nil
	}
	s, ok := domain.Sessions().Get(m.console.id)
	if ok && m.inRepo(s.Info()) {
		return m, nil
	}
	if ok {
		info := s.Info()
		m.statusMsg = i18n.T("%s in %s keeps running in its repository — ctrl+\\ brings it back", info.Label, shortWorktreeName(info.Dir))
	}
	return m.closeConsole(), nil
}
