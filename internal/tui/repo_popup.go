package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/repos"
)

// repoPopup is the transient repo-switcher picker opened with R. It holds an
// MRU snapshot taken at open; ctrl+d edits both the snapshot and the registry.
type repoPopup struct {
	popupMax
	entries   []repos.Entry
	query     string // case-insensitive substring over name+path
	filtering bool   // true while `/` filter sub-mode captures runes
	sel       int    // index into the FILTERED view
	now       time.Time
	mode      dispMode // text display mode; z cycles (cutoff default = no wrapping)
	hscroll   int      // modeScroll horizontal offset
}

// moveSel moves the cursor by d, clamped to the filtered view.
func (p *repoPopup) moveSel(d int) {
	n := p.sel + d
	if hi := len(p.visible()) - 1; n > hi {
		n = hi
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

// openRepoPopup snapshots the registry. With no known repos it sets a status
// hint instead of opening an empty picker.
func (m Model) openRepoPopup() (Model, bool) {
	entries := repos.Load(m.statePath)
	if len(entries) == 0 {
		m.statusMsg = "no known repositories yet (gg records them as you open repos)"
		return m, false
	}
	m = m.pushLayer(&repoPopup{entries: entries, now: time.Now()})
	return m, true
}

// visible returns the filtered entries in display order.
func (p *repoPopup) visible() []repos.Entry {
	if p.query == "" {
		return p.entries
	}
	q := strings.ToLower(p.query)
	out := make([]repos.Entry, 0, len(p.entries))
	for _, e := range p.entries {
		if strings.Contains(strings.ToLower(repos.Name(e)), q) ||
			strings.Contains(strings.ToLower(e.Path), q) {
			out = append(out, e)
		}
	}
	return out
}

// update handles all keys while the picker is open. It swallows everything (no
// fallthrough to global handlers). Navigation-first, like the finder and the
// bookmark/shelf switchers: plain keys navigate, `/` enters a filter sub-mode
// where runes (including `z`) type a query until esc/enter.
func (p *repoPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.filtering {
		// Arrows/pages move the selection live while typing (no cursor reset),
		// like the commit filter; j/k stay query text.
		if filterMotion(msg, p.moveSel, popupFilterPage) {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEsc:
			p.filtering, p.query, p.sel = false, "", 0
		case tea.KeyEnter:
			p.filtering = false // commit: keep the filter, leave input mode
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
			}
			p.sel = 0
		case tea.KeySpace:
			p.query += " "
			p.sel = 0
		case tea.KeyRunes:
			p.query += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
	// Navigation mode. Display-mode + pan keys act here (query chars while filtering).
	if p.handleMaxKey(msg) { // "T" toggles fullscreen (nav mode only)
		return m, nil
	}
	switch msg.String() {
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
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
		return m, nil
	case tea.KeyUp:
		p.moveSel(-1)
		return m, nil
	case tea.KeyDown:
		p.moveSel(1)
		return m, nil
	case tea.KeyPgUp:
		p.moveSel(-popupFilterPage)
		return m, nil
	case tea.KeyPgDown:
		p.moveSel(popupFilterPage)
		return m, nil
	case tea.KeyEnter:
		vis := p.visible()
		m = m.popLayer()
		if p.sel < 0 || p.sel >= len(vis) {
			return m, nil
		}
		target := vis[p.sel].Path
		if samePathTUI(target, m.currentWorktree) {
			return m, nil // already here
		}
		tm, cmd := m.reRoot(target)
		return tm.(Model), cmd
	case tea.KeyCtrlD:
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) {
			return m, nil
		}
		victim := vis[p.sel].Path
		_ = repos.Remove(m.statePath, victim)
		kept := p.entries[:0]
		for _, e := range p.entries {
			if e.Path != victim {
				kept = append(kept, e)
			}
		}
		p.entries = kept
		if n := len(p.visible()); p.sel >= n && n > 0 {
			p.sel = n - 1
		}
		return m, nil
	case tea.KeyRunes:
		switch msg.String() {
		case "/":
			p.filtering = true
		case "j":
			p.moveSel(1)
		case "k":
			p.moveSel(-1)
		}
		return m, nil
	}
	return m, nil
}

// render composites the picker over the layer beneath.
func (p *repoPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the picker box (modal box only).
func (p *repoPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	textW := popupTextWidth(inner)

	header := "Switch repository"
	switch {
	case p.filtering:
		header += "  /" + p.query + "█"
	case p.query != "":
		header += "  /" + p.query
	default:
		header += "   (press / to filter)"
	}

	vis := p.visible()
	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight("  (no match)", textW)}
	} else {
		wr := make([]winRow, len(vis))
		for i, e := range vis {
			marker := "  "
			if samePathTUI(e.Path, m.currentWorktree) {
				marker = "● "
			}
			prefix := "  "
			var st lipgloss.Style
			if i == p.sel {
				prefix = "> "
				st = selectedRow
			}
			row := fmt.Sprintf("%s%s%s  %s  (%s)", prefix, marker, repos.Name(e), e.Path, ageString(p.now, e.LastOpened))
			wr[i] = winRow{text: row, style: st}
		}
		// Cap the visible body; renderWindow scrolls to keep p.sel in view.
		capRows := popupResolveRowCap(p.maximized, termH, 12)
		h := len(vis)
		if h > capRows {
			h = capRows
		}
		bodyLines = renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	}

	hint := []string{"[enter] switch", "[ctrl+d] forget", "[/] filter", "[z] mode", "[T] full", "[esc] close"}
	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "")
	parts = append(parts, wrapParts(hint, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// samePathTUI compares two paths after trimming trailing separators; symlink
// divergence is tolerated as inequality (worst case: a no-op switch into the
// same repo).
func samePathTUI(a, b string) bool {
	return strings.TrimRight(a, "/\\") == strings.TrimRight(b, "/\\")
}

// ageString renders a coarse relative age for the picker rows.
func ageString(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
