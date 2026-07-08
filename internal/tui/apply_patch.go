package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
)

// applyPatchDirMsg carries the resolved default patch directory for the
// apply-patch popup's path prefill (the patchResolvedMsg pattern, read side).
// A resolve error is not fatal — the popup opens with an empty prefill.
type applyPatchDirMsg struct {
	dir string
	err error
}

// openApplyPatchPopup resolves the default export dir off-thread (the same
// directory export-as-patch writes into — the natural place a patch lives),
// then opens the editable-path popup via applyPatchDirMsg.
func (m Model) openApplyPatchPopup() (Model, tea.Cmd) {
	svc := m.svc
	return m, func() tea.Msg {
		dir, err := svc.ExportDefaultDir(context.Background())
		return applyPatchDirMsg{dir: dir, err: err}
	}
}

// applyPatchPopup is the editable-path prompt behind the palette's
// "Apply patch…": enter dispatches engine.ApplyPatch in Auto mode (a mailbox
// forks the working-tree/commits decision via the standard modal Decider).
// Mirrors exportPatchPopup.
type applyPatchPopup struct {
	popupMax
	path textfield
}

func (p *applyPatchPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		path := strings.TrimSpace(p.path.Value())
		if path == "" {
			return m, nil
		}
		m = m.popLayer()
		return m.startOp(engine.ApplyPatch{Path: path, Mode: engine.ApplyModeAuto})
	default:
		p.path.HandleEditKey(msg)
	}
	return m, nil
}

func (p *applyPatchPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString("Apply patch\n\n")
	b.WriteString(viewField("path: ", p.path, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[type] path  [enter] apply  [esc] cancel")
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
