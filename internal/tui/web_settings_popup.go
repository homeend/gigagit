package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/i18n"
)

// webSettingsPopup is the Settings "Web page" editor: the URL line, the
// [web] serve toggle, the [web] addr field and an Open in browser action.
// Both settings are GLOBAL (the page is per human, like the theme).
type webSettingsPopup struct {
	sel     int
	editing bool
	buf     textfield
}

const (
	webRowServe = iota
	webRowAddr
	webRowOpen
	webRowCount
)

func (m Model) openWebSettings() Model {
	return m.pushLayer(&webSettingsPopup{})
}

func (p *webSettingsPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if p.editing {
		switch msg.String() {
		case "esc":
			p.editing = false
			return m, nil
		case "enter":
			p.editing = false
			return m.setWebAddr(strings.TrimSpace(p.buf.Value())), nil
		case "ctrl+c":
			return m, tea.Quit
		}
		p.buf.HandleEditKey(msg)
		return m, nil
	}
	switch msg.String() {
	case "esc":
		return m.popLayer(), nil
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if p.sel > 0 {
			p.sel--
		}
	case "down", "j":
		if p.sel < webRowCount-1 {
			p.sel++
		}
	case "enter", " ":
		switch p.sel {
		case webRowServe:
			return m.toggleWebServe(), nil
		case webRowAddr:
			p.editing, p.buf = true, newTextField(m.cfg.Web.Addr)
		case webRowOpen:
			return m.openInBrowser()
		}
	}
	return m, nil
}

// toggleWebServe flips [web] serve in the global config (effective at the
// next launch; the running page is not touched).
func (m Model) toggleWebServe() Model {
	next := !m.cfg.Web.Serve
	m.cfg.Web.Serve = next
	if err := config.SetWebServe(config.DefaultGlobalPath(), next); err != nil {
		m.statusMsg = i18n.T("web page: serve at startup → %s (not saved: %s)", onOff(next), err.Error())
	} else {
		m.statusMsg = i18n.T("web page: serve at startup %s", onOff(next))
	}
	return m
}

// setWebAddr persists [web] addr ("" = back to a random port); the running
// page keeps its current address.
func (m Model) setWebAddr(addr string) Model {
	m.cfg.Web.Addr = addr
	if err := config.SetWebAddr(config.DefaultGlobalPath(), addr); err != nil {
		m.statusMsg = i18n.T("web page: address not saved: %s", err.Error())
		return m
	}
	if addr == "" {
		m.statusMsg = i18n.T("web page: address → a random port (next start)")
	} else {
		m.statusMsg = i18n.T("web page: address → %s (next start)", addr)
	}
	return m
}

func (p *webSettingsPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	mark := func(i int, text string) string {
		if p.sel == i && !p.editing {
			return st().selectedRow.Render("> " + text)
		}
		return "  " + text
	}
	addr := m.cfg.Web.Addr
	if addr == "" {
		addr = i18n.T("random port")
	}
	rows := []string{
		st().titleStyle.Render(i18n.T("Web page")),
		i18n.T("URL: %s", m.webStatusText()),
		"",
		mark(webRowServe, i18n.T("Serve at startup: %s", onOff(m.cfg.Web.Serve))),
	}
	if p.editing {
		rows = append(rows, "> "+i18n.T("Address: ")+p.buf.View(true))
	} else {
		rows = append(rows, mark(webRowAddr, i18n.T("Address: %s", addr)))
	}
	rows = append(rows, mark(webRowOpen, i18n.T("Open in browser")), "")
	if p.editing {
		rows = append(rows, i18n.T("[type] edit  [enter] save  [esc] cancel"))
	} else {
		rows = append(rows, i18n.T("[enter] change  [esc] back"))
	}
	box := st().modalStyle.Width(popupInnerWidth(w)).Render(strings.Join(rows, "\n"))
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
