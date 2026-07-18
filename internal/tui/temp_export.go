package tui

import (
	"context"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// tempExportResolvedMsg carries the resolved export payload (target dir +
// files) back to the UI thread so the editable-destination popup can open
// prefilled. err is set when resolving the base dir or the entry/bookmark's
// files failed.
type tempExportResolvedMsg struct {
	dir   string
	files []model.ExportFile
	err   error
}

// startTempExportShelf resolves a shelf entry's files + default target dir
// off-thread (TempExportBase + ExportShelfEntry), then delivers
// tempExportResolvedMsg so the editable destination popup can open.
func (m Model) startTempExportShelf(e model.ShelfEntry) (Model, tea.Cmd) {
	svc := m.svc
	return m, func() tea.Msg {
		ctx := context.Background()
		base, err := svc.TempExportBase(ctx)
		if err != nil {
			return tempExportResolvedMsg{err: err}
		}
		files, name, err := svc.ExportShelfEntry(ctx, e)
		if err != nil {
			return tempExportResolvedMsg{err: err}
		}
		return tempExportResolvedMsg{dir: filepath.Join(base, name), files: files}
	}
}

// startTempExportBookmark is the bookmark variant of startTempExportShelf.
func (m Model) startTempExportBookmark(b model.Bookmark) (Model, tea.Cmd) {
	svc := m.svc
	return m, func() tea.Msg {
		ctx := context.Background()
		base, err := svc.TempExportBase(ctx)
		if err != nil {
			return tempExportResolvedMsg{err: err}
		}
		files, name, err := svc.ExportBookmark(ctx, b)
		if err != nil {
			return tempExportResolvedMsg{err: err}
		}
		return tempExportResolvedMsg{dir: filepath.Join(base, name), files: files}
	}
}

// tempExportPopup is the editable-destination confirmation shown after a
// shelf entry's or bookmark's files + default target dir have been resolved.
// dest is prefilled with <base>/<name>; enter runs engine.ExportToDir with
// the (possibly edited) destination.
type tempExportPopup struct {
	popupMax
	dest  textfield
	files []model.ExportFile
}

func (p *tempExportPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		dir := strings.TrimSpace(p.dest.Value())
		if dir == "" || len(p.files) == 0 {
			return m, nil
		}
		files := p.files
		m = m.popLayer() // switcher (if any) beneath stays visible during the write
		return m.startOp(engine.ExportToDir{Dir: dir, Files: files})
	default:
		p.dest.HandleEditKey(msg)
	}
	return m, nil
}

func (p *tempExportPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString(i18n.T("Copy to temp dir") + "\n\n")
	b.WriteString(viewField(i18n.T("dir: "), p.dest, true, popupContentWidth(w)) + "\n\n")
	b.WriteString(i18n.T("[type] dir  [enter] write  [esc] cancel"))
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
