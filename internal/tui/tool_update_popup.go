package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/promptstate"
)

// toolUpdatePopup reviews one tool-template offer: the reason and the new
// block's full text (user ruling: no diff), answered take / keep / edit.
// Nothing is written unless the user takes it.
type toolUpdatePopup struct {
	popupMax
	st  domain.ToolTemplateStatus
	top int // first visible line of the template text (it scrolls; the answer keys never leave the screen)
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
	case tea.KeyUp:
		if p.top > 0 {
			p.top--
		}
		return m, nil
	case tea.KeyDown:
		p.top++ // clamped by box()
		return m, nil
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "t":
			m = m.popLayer()
			if err := domain.ApplyToolUpdate(p.st); err != nil {
				m.statusMsg = i18n.T("external tools: %s", err.Error())
				return m, m.refreshToolStatusesCmd()
			}
			m.statusMsg = i18n.T("external tools: updated %s in %s", p.st.Block.Name, p.st.Path)
			m = m.settleToolStatus(p.st)
			return m.reloadToolConfig(), m.refreshToolStatusesCmd()
		case "k":
			m = m.popLayer()
			if m.promptStore != nil {
				if err := m.promptStore.DeclineToolUpdate(p.st.OfferKey()); err != nil {
					m.statusMsg = i18n.T("external tools: %s", err.Error())
					return m, nil
				}
			}
			// Copy-on-write: the map is shared with earlier Model copies.
			declined := make(map[string]bool, len(m.declinedToolUpdates)+1)
			for k := range m.declinedToolUpdates {
				declined[k] = true
			}
			declined[promptstate.ToolUpdateID(p.st.OfferKey())] = true
			m.declinedToolUpdates = declined
			m.statusMsg = i18n.T("external tools: kept your %s — not offered again until the template changes", p.st.Block.Name)
			return m.rebuildNotices(), m.refreshToolStatusesCmd()
		case "e":
			m = m.popLayer()
			line := config.ToolBlockLine(p.st.Path, p.st.Block.Key())
			return m, handover(editorCommandAt(resolveEditor(), p.st.Path, line), func(err error) tea.Msg {
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
	w, h := m.overlayDims()
	// Wider than the default popup: the template is shell text whose lines
	// read best unwrapped.
	normal := popupInnerWidth(w)
	if wide := w - 8; wide > normal {
		normal = min(wide, 120)
	}
	inner := popupResolveWidth(w, p.maximized, normal)
	tw := popupTextWidth(inner)
	var head []string
	head = append(head, i18n.T("Update %s?", p.st.Block.Name), "")
	for _, ln := range wrapWords(m.toolUpdateReason(p.st), tw) {
		head = append(head, st().dim.Render(ln))
	}
	// The path is elided to what the label leaves free, so the line never
	// wraps (and the file name always shows).
	label := i18n.T("New template (written to %s):", "")
	head = append(head, "", i18n.T("New template (written to %s):", elidePath(p.st.Path, max(tw-lipgloss.Width(label), 12))), "")
	body := strings.Split(renderToolBlockText(p.st.New, tw), "\n")
	hint := i18n.T("[t] take new  [k] keep mine  [e] edit config  [esc] later")
	hints := []string{hint}
	if lipgloss.Width(hint) > tw {
		hints = wrapWords(hint, tw)
	}
	scrollable := false
	// The box frame + padding cost is the modal style's vertical frame.
	room := h - st().modalStyle.GetVerticalFrameSize() - len(head) - 1 - len(hints) - 1
	if room < 3 {
		room = 3
	}
	if len(body) > room {
		scrollable = true
		room-- // the scroll marker line
		if p.top > len(body)-room {
			p.top = len(body) - room
		}
		body = body[p.top : p.top+room]
	} else {
		p.top = 0
	}
	lines := append(head, body...)
	if scrollable {
		lines = append(lines, st().dim.Render(i18n.T("[↑/↓] scroll the template")))
	}
	lines = append(lines, "")
	lines = append(lines, hints...)
	return st().modalStyle.Width(inner).Render(strings.Join(lines, "\n")) + "\n"
}

// renderToolBlockText is the block exactly as the config writer lays it out
// (stamp keys included), each line word-wrapped at w.
func renderToolBlockText(tc config.ToolCommand, w int) string {
	var out []string
	for _, ln := range strings.Split(strings.TrimRight(config.RenderToolCommand(tc), "\n"), "\n") {
		for _, seg := range wrapWords(ln, w) {
			// A token longer than the width (the fingerprint) is split by
			// cells so no row can wrap again inside the box.
			for lipgloss.Width(seg) > w {
				cut := ansi.Truncate(seg, w, "")
				out = append(out, cut)
				seg = strings.TrimPrefix(seg, cut)
			}
			out = append(out, seg)
		}
	}
	return strings.Join(out, "\n")
}

// settleToolStatus marks a taken offer current right away, so the Settings
// row and the notice stop offering it before the background re-read lands.
func (m Model) settleToolStatus(taken domain.ToolTemplateStatus) Model {
	sts := append([]domain.ToolTemplateStatus(nil), m.toolStatuses...)
	for i := range sts {
		if sts[i].Block.Key() == taken.Block.Key() && sts[i].Path == taken.Path {
			sts[i].Block, sts[i].Kind, sts[i].Edited = taken.New, domain.ToolCurrent, false
			sts[i].FromVersion, sts[i].FromRange = taken.ToVersion, taken.ToRange
		}
	}
	m.toolStatuses = sts
	if p := layerOf[*settingsPopup](m); p != nil && p.toolsView {
		m.attachToolStatuses(p)
	}
	return m.rebuildNotices()
}
