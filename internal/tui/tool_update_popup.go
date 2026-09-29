package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// toolUpdatePopup reviews one tool-template offer: the reason and the new
// block's full text (user ruling: no diff), answered take / keep / edit.
// Nothing is written unless the user takes it.
type toolUpdatePopup struct {
	popupMax
	st domain.ToolTemplateStatus
}

// toolUpdateReason is the one-line offer reason shared by this popup and the
// notices.
func (m Model) toolUpdateReason(st domain.ToolTemplateStatus) string {
	if st.Kind == domain.ToolUnsupported {
		return i18n.T("%s %s is outside every version range this template supports", st.ToolLabel, st.AgentVersion)
	}
	var parts []string
	switch {
	case st.FromVersion == 0:
		parts = append(parts, i18n.T("written before template versions — current template v%d", st.ToVersion))
	case st.FromVersion < st.ToVersion:
		parts = append(parts, i18n.T("template updated (v%d → v%d)", st.FromVersion, st.ToVersion))
	}
	if st.FromVersion > 0 && st.FromRange != st.ToRange && st.AgentVersion != "" {
		parts = append(parts, i18n.T("%s %s detected — this block was written for %s", st.ToolLabel, st.AgentVersion, st.FromRange))
	}
	if st.Edited && st.FromVersion > 0 {
		parts = append(parts, i18n.T("you changed this block"))
	}
	return strings.Join(parts, " · ")
}

// toolConfigEditedMsg: the user closed their editor on a config file opened
// from the review; reload the config and the statuses.
type toolConfigEditedMsg struct{ err error }

func (p *toolUpdatePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "t":
			m = m.popLayer()
			if err := domain.ApplyToolUpdate(p.st); err != nil {
				m.statusMsg = i18n.T("external tools: %s", err.Error())
				return m, m.refreshToolStatusesCmd()
			}
			m.statusMsg = i18n.T("external tools: updated %s in %s", p.st.Block.Name, p.st.Path)
			return m.reloadToolConfig(), m.refreshToolStatusesCmd()
		case "k":
			m = m.popLayer()
			if m.promptStore != nil {
				if err := m.promptStore.DeclineToolUpdate(p.st.OfferKey()); err != nil {
					m.statusMsg = i18n.T("external tools: %s", err.Error())
					return m, nil
				}
			}
			m.statusMsg = i18n.T("external tools: kept your %s — not offered again until the template changes", p.st.Block.Name)
			return m, m.refreshToolStatusesCmd()
		case "e":
			m = m.popLayer()
			return m, handover(editorCommandAt(resolveEditor(), p.st.Path, 0), func(err error) tea.Msg {
				return toolConfigEditedMsg{err: err}
			})
		}
	}
	return m, nil
}

func (p *toolUpdatePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *toolUpdatePopup) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	tw := popupTextWidth(inner)
	var b strings.Builder
	b.WriteString(i18n.T("Update %s?", p.st.Block.Name) + "\n\n")
	for _, ln := range wrapWords(m.toolUpdateReason(p.st), tw) {
		b.WriteString(st().dim.Render(ln) + "\n")
	}
	b.WriteString("\n" + i18n.T("New template (written to %s):", elidePath(p.st.Path, tw)) + "\n\n")
	b.WriteString(renderToolBlockText(p.st.New, tw))
	b.WriteString("\n\n" + i18n.T("[t] take new  [k] keep mine  [e] edit config  [esc] later"))
	return st().modalStyle.Width(inner).Render(b.String()) + "\n"
}

// renderToolBlockText is the block exactly as the config writer lays it out
// (stamp keys included), each line word-wrapped at w.
func renderToolBlockText(tc config.ToolCommand, w int) string {
	var out []string
	for _, ln := range strings.Split(strings.TrimRight(config.RenderToolCommand(tc), "\n"), "\n") {
		out = append(out, wrapWords(ln, w)...)
	}
	return strings.Join(out, "\n")
}
