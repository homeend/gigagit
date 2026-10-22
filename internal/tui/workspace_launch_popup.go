package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

// workspaceLaunchPopup is the launch question (spec §3): the opened
// repository belongs to one or more workspaces — open one of them, or the
// repository alone. It sits over the loaded single-repository screen, so
// esc (alone) costs nothing. Asked once per process (maybeAskWorkspace).
type workspaceLaunchPopup struct {
	popupMax
	rows []wsLaunchRow
	sel  int
}

// wsLaunchRow is one choice: a workspace, or alone (entry == nil).
type wsLaunchRow struct {
	entry *workspace.Entry
	label string
}

// maybeAskWorkspace pushes the launch dialog once the repository slot and
// its main checkout are known (the first health probe): when the opened
// repository is a member of ≥ 1 workspace, no workspace is open, nothing
// was asked for by flag, and nothing else owns the screen. A member of
// nothing is marked asked at once; a screen that is busy is retried by the
// next probe.
func (m Model) maybeAskWorkspace() (Model, tea.Cmd) {
	if m.workspaceAsked || m.ws != nil || m.startWorkspace != nil || m.quiet && m.statePath == "" {
		return m, nil
	}
	home := m.homeRepo()
	if home == nil || m.home == "" {
		return m, nil
	}
	containing := workspace.Containing(workspace.Load(m.workspacesPath()), model.KeyOf(home.mainPath))
	if len(containing) == 0 {
		m.workspaceAsked = true
		return m, nil
	}
	if m.topLayer() != nil || m.modal != nil || m.proc != nil || !m.opsIdle() {
		return m, nil
	}
	m.workspaceAsked = true
	p := &workspaceLaunchPopup{}
	for i := range containing {
		e := containing[i]
		names := make([]string, 0, len(e.Repos))
		for _, r := range e.Repos {
			names = append(names, shortWorktreeName(r))
		}
		p.rows = append(p.rows, wsLaunchRow{entry: &e, label: i18n.T("Open workspace %s — %s", e.Name, strings.Join(names, ", "))})
	}
	p.rows = append(p.rows, wsLaunchRow{label: i18n.T("Open %s alone", home.name)})
	return m.pushLayer(p), nil
}

func (p *workspaceLaunchPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	n := len(p.rows)
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc: // alone — never trap: the repository is on screen already
		return m.popLayer(), nil
	case tea.KeyUp:
		p.sel = (p.sel - 1 + n) % n
		return m, nil
	case tea.KeyDown:
		p.sel = (p.sel + 1) % n
		return m, nil
	case tea.KeyEnter:
		row := p.rows[p.sel]
		m = m.popLayer()
		if row.entry == nil {
			return m, nil
		}
		return m.openWorkspace(*row.entry)
	}
	switch msg.String() {
	case "j":
		p.sel = (p.sel + 1) % n
	case "k":
		p.sel = (p.sel - 1 + n) % n
	}
	return m, nil // swallow everything else — no fallthrough to global keys
}

func (p *workspaceLaunchPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	textW := popupTextWidth(inner)
	var b strings.Builder
	b.WriteString(i18n.T("This repository belongs to a workspace") + "\n\n")
	for _, line := range wrapWidth(i18n.T("Open it together with the other repositories of a workspace, or on its own?"), textW, 1<<20) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
	for i, row := range p.rows {
		prefix := "  "
		if i == p.sel {
			prefix = "> "
		}
		line := elideNameMiddle(prefix+row.label, textW)
		if i == p.sel {
			line = st().selectedRow.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] open  [esc] open alone"))
	box := st().modalStyle.Width(inner).Render(strings.TrimRight(b.String(), "\n")) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
