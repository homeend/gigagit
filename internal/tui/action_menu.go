package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// actionRow is one runnable action in the . menu: its stable id, the key that
// runs it, and the footer-style label.
type actionRow struct {
	id    string
	key   string
	label string
}

// availableActions returns the currently-available CONTEXT actions as menu
// rows: row-scoped first, then window-scoped, registry order within each group.
// Global (whole-app) actions are excluded — they live in the footer tail and
// have their own hotkeys. Navigation (id == "") is skipped. The dynamic copy
// rows (contextCopyRows) lead the row group.
func availableActions(m Model) []actionRow {
	var row, window []actionRow
	for _, b := range contextBindings {
		if b.id == "" || !b.when(m) {
			continue
		}
		switch b.scope {
		case scopeRow:
			row = append(row, actionRow{id: b.id, key: b.key, label: b.label})
		case scopeWindow:
			window = append(window, actionRow{id: b.id, key: b.key, label: b.label})
		}
	}
	out := append(m.contextCopyRows(), row...)
	return append(out, window...)
}

// contextCopyRows is fleshed out in Task 3; the empty stub keeps Task 2 green.
func (m Model) contextCopyRows() []actionRow { return nil }

// synthKey reproduces the keypress that runs an action's key, for replay
// through Update. enter/space are the only non-rune keys any action id carries;
// everything else (single runes incl. / , ? .) is a KeyRunes.
func synthKey(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}

// actionMenu is the . overlay: a window-primitive list of runnable actions.
type actionMenu struct {
	rows    []actionRow
	sel     int
	typing  bool // / filter input
	query   string
	mode    dispMode
	hscroll int
}

func (a *actionMenu) visible() []actionRow {
	if a.query == "" {
		return a.rows
	}
	q := strings.ToLower(a.query)
	var out []actionRow
	for _, r := range a.rows {
		if strings.Contains(strings.ToLower(r.label), q) {
			out = append(out, r)
		}
	}
	return out
}

func (a *actionMenu) move(d int) {
	n := len(a.visible())
	a.sel += d
	if a.sel > n-1 {
		a.sel = n - 1
	}
	if a.sel < 0 {
		a.sel = 0
	}
}

// openActionMenu builds the menu from the available actions, narrowed by the
// menu_actions allowlist when set.
func (m Model) openActionMenu() Model {
	rows := availableActions(m)
	if ids := m.cfg.UI.MenuActions; len(ids) > 0 {
		byID := make(map[string]actionRow, len(rows))
		for _, r := range rows {
			byID[r.id] = r
		}
		ordered := make([]actionRow, 0, len(ids))
		for _, id := range ids {
			if r, ok := byID[id]; ok {
				ordered = append(ordered, r)
			}
		}
		rows = ordered
	}
	m.actionMenu = &actionMenu{rows: rows}
	return m
}

// runVisibleRow closes the menu and replays the row's key through Update, which
// reaches the base-layout handler (the menu is now nil).
func (m Model) runVisibleRow(sel int) (tea.Model, tea.Cmd) {
	vis := m.actionMenu.visible()
	if sel < 0 || sel >= len(vis) {
		m.actionMenu = nil
		return m, nil
	}
	key := vis[sel].key
	m.actionMenu = nil
	return m.Update(synthKey(key))
}

func (m Model) updateActionMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.actionMenu
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if a.typing { // / filter input captures keys
		switch msg.Type {
		case tea.KeyEsc:
			a.typing = false
			a.query = ""
			a.sel = 0
		case tea.KeyEnter:
			return m.runVisibleRow(a.sel)
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(a.query); len(r) > 0 {
				a.query = string(r[:len(r)-1])
			}
			a.sel = 0
		case tea.KeyRunes:
			a.query += string(msg.Runes)
			a.sel = 0
		}
		return m, nil
	}
	switch msg.String() {
	case "z":
		a.mode = a.mode.next()
		a.hscroll = 0
		return m, nil
	case "shift+left":
		if a.mode == modeScroll && a.hscroll > 0 {
			if a.hscroll -= m.hscrollStep(); a.hscroll < 0 {
				a.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if a.mode == modeScroll {
			a.hscroll += m.hscrollStep()
		}
		return m, nil
	case "esc", "q":
		// Close like every other popup; q must NOT fall through to the quit row.
		m.actionMenu = nil
		return m, nil
	case "/":
		a.typing = true
		a.query = ""
		a.sel = 0
		return m, nil
	case "up", "k":
		a.move(-1)
		return m, nil
	case "down", "j":
		a.move(1)
		return m, nil
	case "enter":
		return m.runVisibleRow(a.sel)
	}
	// Direct key: run the visible row whose key matches. Space reports its
	// String() as " ", so normalize it to the registry's "space".
	pressed := msg.String()
	if msg.Type == tea.KeySpace {
		pressed = "space"
	}
	vis := a.visible()
	for i, r := range vis {
		if r.key == pressed {
			return m.runVisibleRow(i)
		}
	}
	return m, nil
}

// renderActionMenu draws the overlay (composited by render via overlayCenter),
// mirroring renderRepoPopup.
func (m Model) renderActionMenu() string {
	a := m.actionMenu
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	vis := a.visible()
	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight("  (no match)", textW)}
	} else {
		wr := make([]winRow, len(vis))
		for i, r := range vis {
			prefix := "  "
			var st lipgloss.Style
			if i == a.sel {
				prefix, st = "> ", selectedRow
			}
			wr[i] = winRow{text: prefix + r.label, style: st}
		}
		h := len(vis)
		if h > 14 {
			h = 14
		}
		bodyLines = renderWindow(wr, winOpts{w: textW, h: h, mode: a.mode, anchor: a.sel, hscroll: a.hscroll})
	}
	header := "Actions"
	if a.typing {
		header += "  /" + a.query + "█"
	} else if a.query != "" {
		header += "  /" + a.query
	}
	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "", "[key]/[enter] run  [/] filter  [z] mode  [esc] close")
	return popupBox(inner, strings.Join(parts, "\n"))
}
