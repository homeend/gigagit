package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// shelfSetNamePopup collects a name for a marked SET of working files before
// shelving them as one files entry (mirrors commitNamePopup for a commit). The
// field is pre-filled with "WIP on <branch>" — the stash popup's default, since
// this is the same "park unfinished work" moment; enter shelves the set under
// that name (an empty name is allowed: the row then shows the origin only);
// esc cancels.
type shelfSetNamePopup struct {
	popupMax
	addrs []model.FileAddress
	name  textfield
}

func (p *shelfSetNamePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		label := strings.TrimSpace(p.name.Value())
		addrs := p.addrs
		m = m.popLayer()
		return m, m.shelfAddFilesCmd(addrs, label)
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
}

func (p *shelfSetNamePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString(i18n.T("Shelf %d files as one set", len(p.addrs)) + "\n\n")
	b.WriteString(viewField(i18n.T("name: "), p.name, true, popupContentWidth(w)) + "\n\n")
	b.WriteString(i18n.T("[enter] shelf   [esc] cancel"))
	box := st().modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
