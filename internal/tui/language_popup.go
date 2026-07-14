package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/i18n"
)

// languagePickerPopup selects the TUI display language: the embedded
// bundles plus any custom $XDG_CONFIG_HOME/gg/lang/<code>.toml files.
// Selecting applies immediately (next frame renders translated) and
// persists to the GLOBAL config — a language is per-human, not per-repo.
type languagePickerPopup struct {
	popupMax
	langs []i18n.Lang
	sel   int
}

// openLanguagePicker pushes the picker with the active language selected.
func (m Model) openLanguagePicker() (Model, tea.Cmd) {
	p := &languagePickerPopup{langs: i18n.Available(config.LangDir())}
	for i, l := range p.langs {
		if l.Code == i18n.ActiveCode() {
			p.sel = i
			break
		}
	}
	return m.pushLayer(p), nil
}

func (p *languagePickerPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	n := len(p.langs)
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc: // never trap: esc = keep the current language
		return m.popLayer(), nil
	case tea.KeyUp:
		p.sel = (p.sel - 1 + n) % n
		return m, nil
	case tea.KeyDown:
		p.sel = (p.sel + 1) % n
		return m, nil
	case tea.KeyEnter:
		l := p.langs[p.sel]
		m = m.popLayer()
		if err := i18n.SetLanguage(l.Code, config.LangDir()); err != nil {
			// fail soft: stay on the previous language, report why
			m.statusMsg = "language: " + err.Error()
			return m, nil
		}
		m.cfg.UI.Language = l.Code
		if err := config.SetGlobalUILanguage(config.DefaultGlobalPath(), l.Code); err != nil {
			m.statusMsg = "language: " + l.Name + " (not saved: " + err.Error() + ")"
		} else {
			m.statusMsg = "language: " + l.Name
		}
		return m, nil
	}
	return m, nil // swallow everything else — no fallthrough to global keys
}

func (p *languagePickerPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	var b strings.Builder
	b.WriteString("Language\n\n")
	for i, l := range p.langs {
		prefix := "  "
		if i == p.sel {
			prefix = "> "
		}
		mark := "  "
		if l.Code == i18n.ActiveCode() {
			mark = "● "
		}
		row := prefix + mark + l.Name + " (" + l.Code + ")"
		if i == p.sel {
			row = selectedRow.Render(row)
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n[↑/↓] select  [enter] choose  [esc] cancel")
	box := modalStyle.Width(inner).Render(strings.TrimRight(b.String(), "\n")) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
