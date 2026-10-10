package tui

// Opening an agent tour (stage 4): a worker's row `.` menu offers Open
// brief / Open report; the tour lives in the worker's worktree, so gg
// switches there first and shows it once the switch has loaded
// (consoleSwitch.tour, settled with the console in settleConsoleAfterSwitch).

import (
	"github.com/homeend/gigagit/internal/model"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// tourMenuRows are a session row's Open brief / Open report, for the tours
// the session has.
func (m Model) tourMenuRows(id domain.SessionID) []actionRow {
	brief, report := domain.AgentTourKinds(domain.FullSessionID(id))
	var rows []actionRow
	if brief {
		rows = append(rows, actionRow{id: "session-brief", label: i18n.T("Open brief"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openTour(id, "brief")
		}})
	}
	if report {
		rows = append(rows, actionRow{id: "session-report", label: i18n.T("Open report"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openTour(id, "report")
		}})
	}
	return rows
}

// openTour opens id's brief or report tour: filed again when the user
// closed it, shown at once on the current worktree, else after switching to
// the session's worktree.
func (m Model) openTour(id domain.SessionID, kind string) (Model, tea.Cmd) {
	if m.docs == nil {
		return m, nil
	}
	o, err := m.fileTour(id, kind)
	if err != nil {
		m.statusMsg = i18n.T("no tour to open: %s", err.Error())
		return m, nil
	}
	s, ok := domain.Sessions().Get(id)
	if !ok {
		return m, nil
	}
	dir := s.Info().Dir
	check := m.checkTourCmd([]string{o.ID})
	if model.SamePath(dir, m.currentWorktree) {
		m = m.syncOverviews()
		return m.showTour(o.ID), check
	}
	if m.isRepoWorktree(dir) {
		// This repository: a slot swap; the tour shows once the slot's
		// status (and with it the overviews sync) has landed.
		nm, ok := m.switchView(dir)
		if !ok {
			return nm, nil
		}
		m = nm
		m.tour = o.ID
		return m, check
	}
	gen := m.consoleSwitch.gen
	nm, cmd := m.guardedReRoot(dir, false, false)
	m = nm.(Model)
	// Only a switch THIS call made carries the tour; a refusal (said on the
	// status line) leaves an earlier switch in flight, which lands elsewhere.
	if m.consoleSwitch.gen != gen {
		m.tour = o.ID
	}
	return m, tea.Batch(cmd, check)
}

// showTour pushes the viewer on the current worktree's overview ovID.
func (m Model) showTour(ovID string) Model {
	if d, _ := m.findOverview(ovID); d != nil {
		return m.pushLayer(&fileViewer{d})
	}
	return m
}
