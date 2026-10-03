package tui

// Agent tours (stage 4): the TUI files a spawned worker's brief and every
// agent report as overview documents in the session's worktree
// (agentdocs.Store.FileTour, keyed per session), in the background — they
// never open by themselves. Opening them: agent_tours_open.go.

import (
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

// fileBriefTour files a just-spawned worker's brief; a refusal (the cap)
// only says so on the status line — the worker runs either way.
func (m Model) fileBriefTour(id domain.SessionID) Model {
	if m.docs == nil || id == "" {
		return m
	}
	if _, err := m.fileTour(id, "brief"); err != nil {
		m.statusMsg = i18n.T("brief not filed: %s", err.Error())
	}
	return m
}

// fileReportTours files the report tour of every session whose latest
// report is newer than the one last filed — the level, not the notices, so a
// burst past the notice ring is not missed. A tour the user closed comes
// back only with a NEW report.
func (m Model) fileReportTours() Model {
	if m.docs == nil || m.tourSeq == nil {
		return m
	}
	for _, info := range domain.Sessions().List() {
		d, err := domain.AgentTour(domain.FullSessionID(info.ID), "report")
		if err != nil || d.Seq <= (*m.tourSeq)[info.ID] {
			continue
		}
		(*m.tourSeq)[info.ID] = d.Seq
		if _, _, err := m.docs.FileTour(d.Key, d.Root, d.Dir, d.Title, d.Text); err != nil {
			m.statusMsg = i18n.T("report not filed: %s", err.Error())
		}
	}
	return m
}
