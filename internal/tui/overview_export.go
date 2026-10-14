package tui

import (
	"context"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// overviewExportRow is the . menu's "Export as HTML" for overview d (spec
// 2026-10-12-overview-html-export): the page is built off-thread, then the
// export path popup (export_patch.go's, with its own title) asks where to
// write it through engine.ExportFile.
func (m Model) overviewExportRow(d *openFile) actionRow {
	return actionRow{id: "overview-export", label: i18n.T("Export as HTML"), run: func(m Model) (tea.Model, tea.Cmd) {
		src := domain.OverviewSource{ReviewID: d.src.rev}
		switch {
		case d.src.kind == srcCommitOverview:
			src = domain.OverviewSource{OverviewID: d.src.rev}
		case d.src.kind != srcReviewOverview:
			if m.docs == nil {
				return m, nil
			}
			s, err := domain.AgentOverviewSource(m.docs, d.id(), domain.CheckoutKey(m.currentWorktree))
			if err != nil {
				m.statusMsg = i18n.T("export: %s", err.Error())
				return m, nil
			}
			src = s
		}
		return m.startOverviewExport(src)
	}}
}

// reviewOverviewExportRow is "Export as HTML" on the review view's ≡ Overview
// row: the review's stored overview, without opening it first.
func (m Model) reviewOverviewExportRow() (actionRow, bool) {
	st, p := m.filesReview, m.filesView
	if st == nil || st.overview == nil || p == nil || m.diffLayer() != nil {
		return actionRow{}, false
	}
	vis := p.visible()
	if p.sel < 0 || p.sel >= len(vis) || !vis[p.sel].overviewDoc {
		return actionRow{}, false
	}
	src := domain.OverviewSource{ReviewID: st.id}
	return actionRow{id: "overview-export", label: i18n.T("Export as HTML"), run: func(m Model) (tea.Model, tea.Cmd) {
		return m.startOverviewExport(src)
	}}, true
}

// permanentOverviewExportRow exports permanent overview id without opening
// it: the Branches └ Overview row, a PR view's Overviews entry ("Export as
// HTML") and the Commits menu ("Export as HTML: <title>", one per overview).
func permanentOverviewExportRow(rowID, label, id string) actionRow {
	src := domain.OverviewSource{OverviewID: id}
	return actionRow{id: rowID, label: label, run: func(m Model) (tea.Model, tea.Cmd) {
		return m.startOverviewExport(src)
	}}
}

// startOverviewExport is every export row's and key's start: one export at
// a time (a repeat while the page is built does nothing — ctrl+e in View all
// notes can be pressed again at once), a status line while it builds; the
// patchResolvedMsg that answers frees it.
func (m Model) startOverviewExport(src domain.OverviewSource) (Model, tea.Cmd) {
	if m.overviewExporting {
		return m, nil
	}
	cmd := exportOverviewCmd(m.svc, src)
	if cmd == nil {
		return m, nil
	}
	m.overviewExporting = true
	m.statusMsg = i18n.T("exporting the overview…")
	return m, cmd
}

// exportOverviewCmd builds src's page off-thread and resolves the default
// path; patchResolvedMsg then opens the export path popup with its title.
func exportOverviewCmd(svc *domain.Service, src domain.OverviewSource) tea.Cmd {
	if svc == nil {
		return nil
	}
	title := i18n.T("Export overview as HTML")
	return func() tea.Msg {
		ctx := context.Background()
		data, name, err := svc.ExportOverviewHTML(ctx, src)
		if err != nil {
			return patchResolvedMsg{err: err, title: title}
		}
		dir, err := svc.ExportDefaultDir(ctx)
		if err != nil {
			return patchResolvedMsg{err: err, title: title}
		}
		return patchResolvedMsg{data: data, defaultPath: filepath.Join(dir, name), title: title}
	}
}
