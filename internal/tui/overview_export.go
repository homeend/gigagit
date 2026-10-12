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
		if d.src.kind != srcReviewOverview {
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
		svc, title := m.svc, i18n.T("Export overview as HTML")
		return m, func() tea.Msg {
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
	}}
}
