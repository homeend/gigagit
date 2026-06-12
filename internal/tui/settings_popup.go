package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/agentinit"
)

// settingsPopup is the generic Settings surface opened with `,`. v1 has a
// single menu entry (agent-skill setup); the menu/picker split exists so
// future options have a home.
type settingsPopup struct {
	picker  bool // false = menu screen, true = agent picker
	dets    []agentinit.Detection
	checked []bool
	sel     int
}

const settingsMenuAgents = "Set up agent skills (using-gg)"

// openSettings opens the menu screen.
func (m Model) openSettings() Model {
	m.settings = &settingsPopup{}
	return m
}

// openAgentPicker populates the picker from a fresh detection pass. The
// checkbox defaults encode the core rule: already-installed targets start
// checked (apply = refresh); new targets start unchecked (explicit opt-in).
func (m Model) openAgentPicker() Model {
	p := m.settings
	p.dets = agentinit.Detect(m.currentWorktree, m.initHomeDir)
	p.checked = make([]bool, len(p.dets))
	for i, d := range p.dets {
		p.checked[i] = d.Status.Checked()
	}
	p.sel = 0
	p.picker = true
	return m
}

// updateSettingsKey handles all keys while the settings popup is open.
func (m Model) updateSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.settings
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if p.picker {
			p.picker = false
			return m, nil
		}
		m.settings = nil
		return m, nil
	}
	if !p.picker {
		switch msg.Type {
		case tea.KeyEnter:
			return m.openAgentPicker(), nil
		}
		return m, nil // single menu entry: up/down are no-ops in v1
	}
	switch msg.Type {
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
	case tea.KeyDown:
		if p.sel < len(p.dets)-1 {
			p.sel++
		}
	case tea.KeySpace:
		if p.sel >= 0 && p.sel < len(p.checked) {
			p.checked[p.sel] = !p.checked[p.sel]
		}
	case tea.KeyEnter:
		installed, refreshed, failed := 0, 0, 0
		for i, d := range p.dets {
			if !p.checked[i] {
				continue
			}
			if err := agentinit.Install(d); err != nil {
				failed++
				continue
			}
			if d.Status == agentinit.StatusNew {
				installed++
			} else {
				refreshed++
			}
		}
		m.settings = nil
		m.statusMsg = fmt.Sprintf("agent skills: %d installed, %d refreshed", installed, refreshed)
		if failed > 0 {
			m.statusMsg += fmt.Sprintf(", %d failed", failed)
		}
		return m, nil
	}
	return m, nil
}

// renderSettingsPopup draws whichever screen is active.
func (m Model) renderSettingsPopup() string {
	p := m.settings
	var b strings.Builder
	if !p.picker {
		b.WriteString("Settings\n\n")
		b.WriteString("> " + settingsMenuAgents + "\n")
		b.WriteString("\n[enter] open  [esc] close")
	} else {
		b.WriteString("Set up agent skills\n\n")
		if len(p.dets) == 0 {
			b.WriteString("  no supported agents detected\n")
		}
		for i, d := range p.dets {
			cursor := "  "
			if i == p.sel {
				cursor = "> "
			}
			box := "[ ]"
			if p.checked[i] {
				box = "[x]"
			}
			b.WriteString(fmt.Sprintf("%s%s %s — %s\n", cursor, box, d.Agent.Label, d.Status))
		}
		b.WriteString("\n[space] toggle  [enter] apply  [esc] back")
	}
	w := m.width
	if w <= 0 {
		w = 80
	}
	inner := 56
	if max := w - 8; inner > max {
		inner = max
	}
	if inner < 20 {
		inner = 20
	}
	return modalStyle.Width(inner).Render(strings.TrimRight(b.String(), "\n")) + "\n"
}
