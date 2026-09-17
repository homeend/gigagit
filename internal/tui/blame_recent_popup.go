package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/timespan"
)

// blameRecentPopup is the one-field span dialog behind the blame view's d
// key: type "7d", "1d 3h 5m", "36h" or "90m" and enter turns the recent-lines
// highlight on for that span. Modelled on gotoCommitPopup: a span that does
// not parse keeps the popup open with an inline error under the field; esc
// leaves the highlight exactly as it was. Reads and writes Model.blameRecent,
// never blameView state, so the span survives closing blame.
type blameRecentPopup struct {
	popupMax
	input textfield // the span text
	err   string    // inline error from the last failed parse; "" = none
}

// openBlameRecentPopup pushes the span dialog prefilled with the last span
// used (or the default). Only the blame view opens it.
func (m Model) openBlameRecentPopup() (Model, tea.Cmd) {
	return m.pushLayer(&blameRecentPopup{input: newTextField(blameRecentSeed(m.blameRecent))}), nil
}

func (p *blameRecentPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		text := strings.TrimSpace(p.input.Value())
		span, err := timespan.Parse(text)
		if err != nil {
			p.err = i18n.T("not a time span: %s", text)
			return m, nil
		}
		m.blameRecent = blameRecent{on: true, span: span, last: text}
		return m.popLayer(), nil
	default:
		// Spaces are part of the grammar ("1d 3h 5m"): HandleEditKey inserts
		// them like any rune.
		if p.input.HandleEditKey(msg) {
			p.err = "" // editing clears the stale error
		}
	}
	return m, nil
}

func (p *blameRecentPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *blameRecentPopup) box(m Model) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString(i18n.T("Highlight lines changed within the last…") + "\n\n")
	b.WriteString(viewField(i18n.T("span: "), p.input, true, popupContentWidth(w)) + "\n")
	b.WriteString(st().dim.Render(i18n.T("e.g. 7d, 1d 3h 5m, 36h, 90m")) + "\n")
	if p.err != "" {
		b.WriteString("\n" + st().errorText.Render(p.err) + "\n")
	}
	b.WriteString("\n" + i18n.T("[enter] apply  [esc] cancel"))
	return st().modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
