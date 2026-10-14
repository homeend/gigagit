package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// Where a permanent overview is found (spec 2026-10-12-permanent-overviews
// §9): the commit's . menu (from any branch, R5), a └ Overview sub-row
// under a branch while its tip IS the overview's commit, and — when it is
// deleted anywhere — its open split closes on the next counts reload.

// branchOverviewHeads are b's permanent overviews of its CURRENT tip, newest
// first: made on b while its tip was that commit (spec R5).
func (m Model) branchOverviewHeads(b model.Branch) []domain.OverviewHead {
	var out []domain.OverviewHead
	for _, o := range m.noteCounts.Overviews {
		if o.Branch == b.Name && sameCommit(o.Commit, b.Hash) {
			out = append(out, o)
		}
	}
	return out
}

// branchOverviewRowBody is an overview sub-row without its indent:
// "└ Overview: <title> 10-12 09:40 Claude Code".
func branchOverviewRowBody(h domain.OverviewHead) string {
	parts := []string{i18n.T("Overview:"), sanitizeLine(h.Title)}
	if !h.Created.IsZero() {
		parts = append(parts, reviewStampAt(h.Created, clock.Now()))
	}
	if a := strings.TrimSpace(h.Agent); a != "" {
		parts = append(parts, sanitizeLine(a))
	}
	return "└ " + strings.Join(parts, " ")
}

// overviewHead is the commit overview id among the loaded counts.
func (m Model) overviewHead(id string) (domain.OverviewHead, bool) {
	for _, o := range m.noteCounts.Overviews {
		if o.ID == id {
			return o, true
		}
	}
	return domain.OverviewHead{}, false
}

// selectedBranchOverview resolves an overview sub-row under the Branches cursor.
func (m Model) selectedBranchOverview() (domain.OverviewHead, bool) {
	e, ok := m.selectedBranchEntry()
	if !ok || e.overview == "" {
		return domain.OverviewHead{}, false
	}
	return m.overviewHead(e.overview)
}

// commitOverviewRows are the Commits .-menu's "Open overview: <title>" and
// "Export as HTML: <title>", a pair per permanent overview of the selected
// commit, newest first.
func (m Model) commitOverviewRows() []actionRow {
	if m.focus != panelCommits || m.inContentWindow() {
		return nil
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok || bi >= len(m.commits) {
		return nil
	}
	var out []actionRow
	for _, o := range m.noteCounts.Overviews {
		if o.Commit != m.commits[bi].Hash {
			continue
		}
		id := o.ID
		out = append(out, actionRow{id: "open-overview-" + id, label: i18n.T("Open overview: %s", sanitizeLine(o.Title)),
			run: func(m Model) (tea.Model, tea.Cmd) { return m.openCommitOverview(id) }},
			permanentOverviewExportRow("export-overview-"+id, i18n.T("Export as HTML: %s", sanitizeLine(o.Title)), id))
	}
	return out
}

// branchOverviewRows are an overview sub-row's .-menu: Open, Export as
// HTML and Delete.
func (m Model) branchOverviewRows() []actionRow {
	if m.focus != panelBranches || m.inContentWindow() {
		return nil
	}
	h, ok := m.selectedBranchOverview()
	if !ok {
		return nil
	}
	id, quote := h.ID, reviewQuote(h.Agent, h.Title)
	return []actionRow{
		{id: "open-overview", label: i18n.T("Open overview"), run: func(m Model) (tea.Model, tea.Cmd) { return m.openCommitOverview(id) }},
		permanentOverviewExportRow("overview-export", i18n.T("Export as HTML"), id),
		{id: "delete-overview", label: i18n.T("Delete overview"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOverviewDelete(id, quote)
		}},
	}
}

// confirmOverviewDelete asks before a permanent overview goes (it is the
// agent's stored work; gg overview rm is the CLI twin).
func (m Model) confirmOverviewDelete(id, quote string) (tea.Model, tea.Cmd) {
	m.modal = &decisionState{
		req: engine.DecisionRequest{ID: "overview-remove", Prompt: i18n.T("Delete this overview?") + "\n" + quote,
			Options: []string{"Delete", "Cancel"}},
		sel: 1, // default highlight = Cancel
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Delete" || m.svc == nil {
				return m, nil
			}
			svc := m.svc
			return m, func() tea.Msg {
				return storedDeletedMsg{id: id, overview: true, err: svc.RemoveOverview(context.Background(), id)}
			}
		},
	}
	return m, nil
}

// dropGoneOverviews closes every open permanent overview the reloaded
// counts c no longer hold (deleted here, by the CLI or another window):
// its split comes down with it (closeDoc → detachDoc). One open in a
// sleeping worktree slot is marked gone and closes when the slot shows
// again (its windows are not live here).
func (m Model) dropGoneOverviews(c domain.NoteCounts) Model {
	have := map[string]bool{}
	for _, o := range c.Overviews {
		have[o.ID] = true
	}
	for _, hs := range c.PreviewOverviews {
		for _, o := range hs {
			have[o.ID] = true
		}
	}
	return m.markOverviewsGone(func(id string) bool { return !have[id] })
}

// markOverviewsGone marks every open permanent overview gone(id) names in
// this repository's worktree slots (another repository's open files are
// not judged by these counts) and closes the live slot's at once. The mark
// is recomputed on every sweep, so a copy the counts hold again is kept.
func (m Model) markOverviewsGone(gone func(id string) bool) Model {
	for wt, docs := range m.openFiles.all() {
		if _, ours := m.views[wt]; !ours && wt != model.KeyOf(m.currentWorktree) {
			continue
		}
		for _, d := range docs {
			if d.src.kind == srcCommitOverview {
				d.gone = gone(d.src.rev)
			}
		}
	}
	return m.closeGoneDocs()
}

// overviewMarkedGone: an open copy of overview id is already marked gone.
func (m Model) overviewMarkedGone(id string) bool {
	for _, docs := range m.openFiles.all() {
		for _, d := range docs {
			if d.src.kind == srcCommitOverview && d.src.rev == id && d.gone {
				return true
			}
		}
	}
	return false
}

// closeGoneDocs closes the live worktree's documents marked gone.
func (m Model) closeGoneDocs() Model {
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.gone {
			m = m.closeDoc(d)
			m.statusMsg = i18n.T("the overview was deleted")
		}
	}
	return m
}
