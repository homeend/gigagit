package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// relatedPromptPopup asks the ONE follow-up question a Settings toggle can
// trigger (see related_prompts.go). It pushes on top of the Settings popup;
// closing it (any choice, or esc = Not now) returns to Settings with the
// flipped toggle still visible.
type relatedPromptPopup struct {
	popupMax
	prompt *relatedPrompt
	sel    int // 0 = yes, 1 = not now, 2 = don't ask again
}

// maybeRelatedPrompt consults the registry after a Settings toggle applied and
// pushes the follow-up popup when a trigger matches. Call it with the
// setting's FRESH value (after the toggle mutated cfg).
func (m Model) maybeRelatedPrompt(setting, newValue string) (Model, tea.Cmd) {
	rp := m.relatedPromptFor(setting, newValue)
	if rp == nil {
		return m, nil
	}
	return m.pushLayer(&relatedPromptPopup{prompt: rp}), nil
}

// options returns the fixed three-choice list, yes-label first.
func (p *relatedPromptPopup) options() []string {
	return []string{relatedYesLabel(p.prompt.yesLabel), i18n.T("Not now"), i18n.T("No — don't ask again")}
}

func (p *relatedPromptPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	n := len(p.options())
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc: // esc = Not now — never trap, never write
		return m.popLayer(), nil
	case tea.KeyUp:
		p.sel = (p.sel - 1 + n) % n
		return m, nil
	case tea.KeyDown:
		p.sel = (p.sel + 1) % n
		return m, nil
	case tea.KeyEnter:
		sel, rp := p.sel, p.prompt
		m = m.popLayer()
		switch sel {
		case 0:
			return rp.apply(m)
		case 2:
			if m.promptStore == nil {
				m.statusMsg = i18n.T("couldn't save the choice (no state dir) — will ask again")
			} else if err := m.promptStore.SuppressPrompt(rp.id); err != nil {
				m.statusMsg = i18n.T("couldn't save the choice — will ask again (%s)", err.Error())
			} else {
				m.statusMsg = i18n.T("won't ask again — saved to %s", defaultPromptStatePath())
			}
		}
		return m, nil
	}
	return m, nil // swallow everything else — no fallthrough to global keys
}

func (p *relatedPromptPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	textW := popupTextWidth(inner)
	var b strings.Builder
	b.WriteString(i18n.T("Related option") + "\n\n")
	for _, line := range wrapWidth(p.prompt.question, textW, 1<<20) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
	for i, opt := range p.options() {
		prefix := "  "
		if i == p.sel {
			prefix = "> "
		}
		row := prefix + opt
		if i == p.sel {
			row = selectedRow.Render(row)
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] choose  [esc] not now"))
	// Name the state file so a persisted "don't ask again" is discoverable
	// and resettable (delete or edit prompts.toml to bring prompts back).
	if path := defaultPromptStatePath(); path != "" {
		b.WriteString("\n")
		for _, seg := range wrapWidth(i18n.T("don't-ask-again choices: %s", path), textW, 1<<20) {
			b.WriteString(seg + "\n")
		}
	}
	box := modalStyle.Width(inner).Render(strings.TrimRight(b.String(), "\n")) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
