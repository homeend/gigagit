package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/agentinit"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/observ"
)

// settingsPopup is the generic Settings surface opened with `,`. v1 has a
// single menu entry (agent-skill setup); the menu/picker split exists so
// future options have a home.
type settingsPopup struct {
	picker     bool // false = menu screen, true = agent picker
	errorsView bool // true = session-errors viewer screen
	dets       []agentinit.Detection
	checked []bool
	sel     int      // selection within the agent picker list
	menuSel int      // selection within the top-level menu (independent of sel)
	mode    dispMode // text display mode; z cycles (cutoff default)
	hscroll int      // modeScroll horizontal offset
}

const (
	settingsMenuAgents   = "Set up agent skills (using-gg)"
	settingsMenuIdentity = "Identity & profiles"
	settingsMenuPrefixes = "Branch prefixes"
	settingsMenuOpLog    = "Operation log"
	settingsMenuErrors   = "Session errors"
)

// settingsMenu is the top-level menu order.
var settingsMenu = []string{settingsMenuAgents, settingsMenuIdentity, settingsMenuPrefixes, settingsMenuOpLog, settingsMenuErrors}

// settingsMenuLabel renders one menu row. The operation-log row is dynamic: it
// shows the on/off state and the log filename, so the menu both reveals whether
// logging is enabled and tells the user where to find it.
func settingsMenuLabel(m Model, i int) string {
	if settingsMenu[i] == settingsMenuOpLog {
		path := "(no state dir)"
		on := false
		if m.opLog != nil {
			on = m.opLog.on
			if m.opLog.path != "" {
				path = m.opLog.path
			}
		}
		if on {
			return settingsMenuOpLog + ": on — " + path
		}
		return settingsMenuOpLog + ": off (" + path + ")"
	}
	if settingsMenu[i] == settingsMenuErrors {
		path := defaultErrLogPath()
		if path == "" {
			path = "(no state dir)"
		}
		n := len(observ.SessionFailures())
		if n == 0 {
			return settingsMenuErrors + ": none — " + path
		}
		return fmt.Sprintf("%s: %d — %s", settingsMenuErrors, n, path)
	}
	return settingsMenu[i]
}

// toggleOpLog flips the operation log, persisting the choice to the global
// config so it survives restarts (the user's persist-to-config choice).
func (m Model) toggleOpLog() Model {
	if m.opLog == nil {
		m.opLog = newOpLog()
	}
	var err error
	if m.opLog.on {
		err = m.opLog.disable()
	} else {
		err = m.opLog.enable()
	}
	if err != nil {
		m.statusMsg = "operation log: " + err.Error()
		return m
	}
	m.cfg.Debug.LogOperations = m.opLog.on // keep the in-memory view in sync
	if perr := config.SetGlobalDebugLogOperations(config.DefaultGlobalPath(), m.opLog.on); perr != nil {
		m.statusMsg = "operation log toggled but not saved: " + perr.Error()
		return m
	}
	if m.opLog.on {
		m.statusMsg = "operation log on — " + m.opLog.path
	} else {
		m.statusMsg = "operation log off"
	}
	return m
}

// openSettings opens the menu screen.
func (m Model) openSettings() Model {
	m = m.pushLayer(&settingsPopup{})
	return m
}

// openAgentPicker populates the picker from a fresh detection pass. The
// checkbox defaults encode the core rule: already-installed targets start
// checked (apply = refresh); new targets start unchecked (explicit opt-in).
func (m Model) openAgentPicker() Model {
	p := layerOf[*settingsPopup](m)
	p.dets = agentinit.Detect(m.currentWorktree, m.initHomeDir)
	p.checked = make([]bool, len(p.dets))
	for i, d := range p.dets {
		p.checked[i] = d.Status.Checked()
	}
	p.sel = 0
	p.picker = true
	return m
}

// update handles all keys while the settings popup is open.
func (p *settingsPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if p.errorsView {
			p.errorsView = false
			return m, nil
		}
		if p.picker {
			p.picker = false
			return m, nil
		}
		m = m.popLayer()
		return m, nil
	}
	switch msg.String() { // display-mode keys apply on both screens
	case "z":
		p.mode = p.mode.next()
		p.hscroll = 0
		return m, nil
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
		return m, nil
	}
	if !p.picker && !p.errorsView {
		switch msg.Type {
		case tea.KeyUp:
			if p.menuSel > 0 {
				p.menuSel--
			}
			return m, nil
		case tea.KeyDown:
			if p.menuSel < len(settingsMenu)-1 {
				p.menuSel++
			}
			return m, nil
		case tea.KeyEnter:
			switch settingsMenu[p.menuSel] {
			case settingsMenuAgents:
				return m.openAgentPicker(), nil
			case settingsMenuIdentity:
				return m.openIdentityView()
			case settingsMenuPrefixes:
				return m.openPrefixSettings()
			case settingsMenuOpLog:
				return m.toggleOpLog(), nil // stays open so the state flip is visible
			case settingsMenuErrors:
				p.errorsView = true
				p.sel = 0
				p.hscroll = 0
				return m, nil
			}
			return m, nil
		}
		return m, nil
	}
	if p.errorsView {
		fs := observ.SessionFailures()
		switch msg.Type {
		case tea.KeyUp:
			if p.sel > 0 {
				p.sel--
			}
		case tea.KeyDown:
			if p.sel < len(fs)-1 {
				p.sel++
			}
		}
		return m, nil
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
		m = m.popLayer()
		m.statusMsg = fmt.Sprintf("agent skills: %d installed, %d refreshed", installed, refreshed)
		if failed > 0 {
			m.statusMsg += fmt.Sprintf(", %d failed", failed)
		}
		return m, nil
	}
	return m, nil
}

// render composites the popup over the layer beneath.
func (p *settingsPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws whichever screen is active (modal box only).
func (p *settingsPopup) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	var b strings.Builder
	if p.errorsView {
		b.WriteString("Session errors\n\n")
		fs := observ.SessionFailures()
		if len(fs) == 0 {
			b.WriteString("  no errors this session\n")
		} else {
			wr := make([]winRow, len(fs))
			for i, e := range fs {
				prefix := "  "
				var st lipgloss.Style
				if i == p.sel {
					prefix, st = "> ", selectedRow
				}
				wr[i] = winRow{
					text:  fmt.Sprintf("%s%s  %s — %s", prefix, e.Time.Format("15:04:05"), e.Source, e.Detail),
					style: st,
				}
			}
			h := len(fs)
			if h > 12 {
				h = 12
			}
			for _, line := range renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll}) {
				b.WriteString(line + "\n")
			}
		}
		if path := defaultErrLogPath(); path != "" {
			b.WriteString("\nfull history: " + path + "\n")
		}
		b.WriteString("\n[z] mode  [esc] back")
	} else if !p.picker {
		b.WriteString("Settings\n\n")
		for i := range settingsMenu {
			prefix := "  "
			if i == p.menuSel {
				prefix = "> "
			}
			b.WriteString(prefix + settingsMenuLabel(m, i) + "\n")
		}
		// A short static menu (not a renderWindow list), so z has no visible
		// effect here — only the picker advertises [z] mode.
		b.WriteString("\n[↑/↓] select  [enter] open/toggle  [esc] close")
	} else {
		b.WriteString("Set up agent skills\n\n")
		if len(p.dets) == 0 {
			b.WriteString("  no supported agents detected\n")
		} else {
			wr := make([]winRow, len(p.dets))
			for i, d := range p.dets {
				prefix := "  "
				var st lipgloss.Style
				if i == p.sel {
					prefix, st = "> ", selectedRow
				}
				box := "[ ]"
				if p.checked[i] {
					box = "[x]"
				}
				wr[i] = winRow{text: fmt.Sprintf("%s%s %s — %s", prefix, box, d.Agent.Label, d.Status), style: st}
			}
			h := len(p.dets)
			if h > 12 {
				h = 12
			}
			for _, line := range renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll}) {
				b.WriteString(line + "\n")
			}
		}
		b.WriteString("\n[space] toggle  [enter] apply  [z] mode  [esc] back")
	}
	return popupBox(inner, strings.TrimRight(b.String(), "\n"))
}
