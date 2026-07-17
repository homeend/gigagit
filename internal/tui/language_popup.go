package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/i18n"
)

// langHintStyle dims the repo-override warning under the picker title.
var langHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// languagePickerPopup selects the TUI display language: the embedded
// bundles plus any custom $XDG_CONFIG_HOME/gg/lang/<code>.toml files.
// Selecting applies immediately (next frame renders translated) and
// persists to the GLOBAL config — a language is per-human, not per-repo.
type languagePickerPopup struct {
	popupMax
	langs        []i18n.Lang
	sel          int
	repoOverride bool // active repo config sets [ui] language — it beats the global write
}

// openLanguagePicker pushes the picker with the active language selected.
func (m Model) openLanguagePicker() (Model, tea.Cmd) {
	p := &languagePickerPopup{
		langs:        i18n.Available(config.LangDir()),
		repoOverride: config.FileUILanguage(m.repoConfigPath) != "",
	}
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
			m.statusMsg = i18n.T("language failed: %s", err.Error())
			return m, nil
		}
		m.cfg.UI.Language = l.Code
		m = m.rebuildNotices()
		if err := config.SetGlobalUILanguage(config.DefaultGlobalPath(), l.Code); err != nil {
			m.statusMsg = i18n.T("language: %s (not saved: %s)", l.Name, err.Error())
		} else {
			m.statusMsg = i18n.T("language: %s", l.Name)
		}
		return m, nil
	}
	return m, nil // swallow everything else — no fallthrough to global keys
}

func (p *languagePickerPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	var b strings.Builder
	b.WriteString(i18n.T("Language") + "\n")
	if p.repoOverride {
		b.WriteString(langHintStyle.Render(i18n.T("(repo config sets [ui] language — it overrides this choice)")) + "\n")
	}
	b.WriteString("\n")
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
	b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] choose  [esc] cancel"))
	box := modalStyle.Width(inner).Render(strings.TrimRight(b.String(), "\n")) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
