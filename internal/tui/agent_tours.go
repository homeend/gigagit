package tui

// Agent tours (stage 4): the TUI files a spawned worker's brief and every
// agent report as overview documents in the session's worktree
// (agentdocs.Store.FileTour, keyed per session), in the background — they
// never open by themselves. Opening them: agent_tours_open.go.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// fileTour composes id's tour of kind and files (or replaces) it.
func (m Model) fileTour(id domain.SessionID, kind string) (agentdocs.Overview, error) {
	d, err := domain.AgentTour(domain.FullSessionID(id), kind)
	if err != nil {
		return agentdocs.Overview{}, err
	}
	o, _, err := m.docs.FileTour(d.Key, d.Root, d.Dir, d.Title, d.Text)
	return o, err
}

// checkTourCmd has the store check the filed tours' anchors off the UI
// thread (a file anchor stats the worktree); the store's signal repaints.
func (m Model) checkTourCmd(ids []string) tea.Cmd {
	if len(ids) == 0 {
		return nil
	}
	docs := m.docs
	return func() tea.Msg {
		for _, id := range ids {
			docs.CheckAnchors(id)
		}
		return nil
	}
}

// fileBriefTour files a just-spawned worker's brief; a refusal (the cap)
// only says so on the status line — the worker runs either way.
func (m Model) fileBriefTour(id domain.SessionID) (Model, tea.Cmd) {
	if m.docs == nil || id == "" {
		return m, nil
	}
	o, err := m.fileTour(id, "brief")
	if err != nil {
		m.statusMsg = i18n.T("brief not filed: %s", err.Error())
		return m, nil
	}
	return m, m.checkTourCmd([]string{o.ID})
}

// fileReportTours files the report tour of every session whose latest
// report is newer than the one last filed — the level, not the notices, so a
// burst past the notice ring is not missed. A tour the user closed comes
// back only with a NEW report; one refused (the cap) is tried again on the
// next wake, its refusal said once.
func (m Model) fileReportTours() (Model, tea.Cmd) {
	if m.docs == nil || m.tourSeq == nil || m.tourRefused == nil {
		return m, nil
	}
	var filed []string
	for _, info := range domain.Sessions().List() {
		d, err := domain.AgentTour(domain.FullSessionID(info.ID), "report")
		if err != nil || d.Seq <= (*m.tourSeq)[info.ID] {
			continue
		}
		o, _, err := m.docs.FileTour(d.Key, d.Root, d.Dir, d.Title, d.Text)
		if err != nil {
			if (*m.tourRefused)[info.ID] != d.Seq {
				(*m.tourRefused)[info.ID] = d.Seq
				m.statusMsg = i18n.T("report not filed: %s", err.Error())
			}
			continue
		}
		(*m.tourSeq)[info.ID] = d.Seq
		delete(*m.tourRefused, info.ID)
		filed = append(filed, o.ID)
	}
	return m, m.checkTourCmd(filed)
}
