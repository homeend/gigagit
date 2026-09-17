package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/timespan"
)

// blameRecentPopup is the one-field age-filter dialog behind the blame view's
// d key: type "-7d" (younger than a week), "+30d" (older than a month),
// "+1d -7d" (between) or a bare "7d" (≡ -7d) and enter turns the highlight
// on for that window. Modelled on gotoCommitPopup: text that does not parse
// keeps the popup open with an inline error under the field; esc leaves the
// highlight exactly as it was. Enter writes the filter onto the blameView it
// was opened over (bv) and the raw text onto Model.blameRecentLast — the
// on/off state dies with the view, the text seeds the next dialog.
type blameRecentPopup struct {
	popupMax
	bv    *blameView // the view the dialog was opened over; enter writes bv.recent
	input textfield  // the filter text
	err   string     // inline error from the last failed parse; "" = none
	// pristine is true until the first key touches the prefilled text: a typed
	// rune then REPLACES the prefill instead of appending to it ("7d" + typed
	// "3d" must be 3d, never 7d3d) — the web prompt selects its value for the
	// same reason. Backspace, arrows and the rest edit the prefill in place.
	pristine bool
}

// openBlameRecentPopup pushes the age dialog over b, prefilled with the last
// text submitted (or the default). Only the blame view opens it.
func (m Model) openBlameRecentPopup(b *blameView) (Model, tea.Cmd) {
	return m.pushLayer(&blameRecentPopup{bv: b, input: newTextField(blameRecentSeed(m.blameRecentLast)), pristine: true}), nil
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
		f, err := timespan.ParseFilter(text)
		if err != nil {
			p.err = i18n.T("not an age filter: %s", text)
			return m, nil
		}
		p.bv.recent = blameRecent{on: true, f: f}
		m.blameRecentLast = text
		return m.popLayer(), nil
	default:
		if p.pristine && (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace) {
			p.input = newTextField("") // the first typed rune replaces the prefill
		}
		p.pristine = false
		// Spaces are part of the grammar ("+1d 2h -7d"): HandleEditKey inserts
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
	b.WriteString(i18n.T("Highlight lines by age…") + "\n\n")
	b.WriteString(viewField(i18n.T("span: "), p.input, true, popupContentWidth(w)) + "\n")
	b.WriteString(st().dim.Render(i18n.T("-7d younger · +30d older · +1d -7d between · +w = older than a week")) + "\n")
	if p.err != "" {
		b.WriteString("\n" + st().errorText.Render(p.err) + "\n")
	}
	b.WriteString("\n" + i18n.T("[enter] apply  [esc] cancel"))
	return st().modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
