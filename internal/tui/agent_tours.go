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
	"github.com/homeend/gigagit/internal/model"
)

// fileTour composes id's tour of kind and files (or replaces) it — as a
// document in the WORKER's worktree directory (spec
// 2026-10-18-gg-session-docs §6.5), attached first.
func (m Model) fileTour(id domain.SessionID, kind string) (agentdocs.Overview, error) {
	d, err := domain.AgentTour(domain.FullSessionID(id), kind)
	if err != nil {
		return agentdocs.Overview{}, err
	}
	m.attachTourRoot(d.Dir)
	o, _, err := m.docs.FileTour(d.Key, d.Root, d.Dir, d.Title, d.Text)
	return o, err
}

// attachTourRoot attaches a worker's worktree before its tour is filed, so
// the tour lands as a file there (author gg, a fixed name): a worktree of
// this repository, whose docs directory is under this session dir. A worker
// in another repository keeps a memory tour, as before.
func (m Model) attachTourRoot(dir string) {
	if model.SamePath(dir, m.currentWorktree) || m.isRepoWorktree(dir) || m.isMemberWorktree(dir) {
		m.attachDocs(dir) // a worker in another member: its tour is a file there too, as the member's slot will attach that directory
		if m.tourRoots != nil {
			(*m.tourRoots)[domain.CheckoutKey(dir)] = dir // detached with the shown roots
		}
	}
}

// checkTourCmd has the store check the filed tours' anchors off the UI
// thread (a file anchor stats the worktree); the store's signal repaints.
func (m Model) checkTourCmd(tours []agentdocs.Overview) tea.Cmd {
	if len(tours) == 0 {
		return nil
	}
	docs := m.docs
	return func() tea.Msg {
		for _, o := range tours {
			docs.CheckAnchorsIn(o.Root, o.ID)
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
	return m, m.checkTourCmd([]agentdocs.Overview{o})
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
	var filed []agentdocs.Overview
	for _, info := range domain.Sessions().List() {
		d, err := domain.AgentTour(domain.FullSessionID(info.ID), "report")
		if err != nil || d.Seq <= (*m.tourSeq)[info.ID] {
			continue
		}
		m.attachTourRoot(d.Dir)
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
		filed = append(filed, o)
	}
	return m, m.checkTourCmd(filed)
}
