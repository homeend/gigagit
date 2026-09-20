package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
)

// histRow is one copied link as the TUI holds it. A TUI-local copy of the
// domain's history entry, taken at the message boundary, so this package
// never names internal/linkhist (archtest: frontends reach it through domain).
type histRow struct{ link, desc string }

// linkHistMax is how many rows the picker shows. The store's ring holds the
// same number; the clamp here keeps the popup's height bounded whatever a
// future store decides.
const linkHistMax = 20

// linkHistPicker is the copied-link history shown UNDER a link input: the `#`
// prompt today, both fields of the "Compare with link…" dialog next. The host
// owns the text field; the picker owns the list and answers one question —
// "did this key pick a link".
//
// Focus model: the field has the keyboard until ↓ moves into the list
// (active). In the list ↑/↓ move, ↑ on the first row returns to the field,
// enter picks, esc returns to the field WITHOUT closing the host (one esc
// must never both leave the list and lose the popup), and any other key
// returns to the field and is then handled there, so typing never gets lost.
type linkHistPicker struct {
	rows   []histRow
	sel    int
	loaded bool // the load has landed (an empty history is then SAID, not implied)
	active bool // the list, not the field, has the keyboard
}

// linkHistLoadedMsg delivers the history to whichever host asked for it.
type linkHistLoadedMsg struct {
	gen  int
	rows []histRow
}

// linkHistHost is a layer that embeds a picker.
type linkHistHost interface{ histPickers() []*linkHistPicker }

// linkHistCmd reads the history off the UI thread (it is a file read behind a
// cross-process lock — never something to do inside pushLayer).
func (m Model) linkHistCmd(gen int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		hist := svc.LinkHistory(context.Background())
		rows := make([]histRow, 0, len(hist))
		for _, e := range hist {
			rows = append(rows, histRow{link: e.Link, desc: e.Desc})
		}
		return linkHistLoadedMsg{gen: gen, rows: rows}
	}
}

// loadedLinkHist hands a landed history to the host on top — if it is still
// the load that host asked for.
func (m Model) loadedLinkHist(msg linkHistLoadedMsg) (Model, tea.Cmd) {
	if msg.gen != m.linkHistGen {
		return m, nil
	}
	host, ok := m.topLayer().(linkHistHost)
	if !ok {
		return m, nil
	}
	rows := msg.rows
	if len(rows) > linkHistMax {
		rows = rows[:linkHistMax]
	}
	// EVERY picker of the host: the compare dialog has one per link field, and
	// a load that filled only the first would leave the second saying "no
	// copied links yet" beside a full history.
	for _, p := range host.histPickers() {
		p.rows = rows
		p.sel, p.loaded, p.active = 0, true, false
	}
	return m, nil
}

// enter moves the keyboard into the list; it reports false (and does nothing)
// when there is nothing to enter.
func (p *linkHistPicker) enter() bool {
	if len(p.rows) == 0 {
		return false
	}
	p.active, p.sel = true, 0
	return true
}

// key handles one key while the list is active. picked is the chosen link
// ("" = none); handled=false means the key belongs to the field — the list
// has already let go of the keyboard.
func (p *linkHistPicker) key(msg tea.KeyMsg) (picked string, handled bool) {
	if !p.active {
		return "", false
	}
	switch msg.Type {
	case tea.KeyUp:
		if p.sel == 0 {
			p.active = false
		} else {
			p.sel--
		}
		return "", true
	case tea.KeyDown:
		if p.sel < len(p.rows)-1 {
			p.sel++
		}
		return "", true
	case tea.KeyEnter:
		p.active = false
		if p.sel < 0 || p.sel >= len(p.rows) {
			return "", true
		}
		return p.rows[p.sel].link, true
	case tea.KeyEsc:
		p.active = false
		return "", true
	}
	p.active = false
	return "", false
}

// view renders the list at most width columns wide. Each row is
// "<desc>  <link>": the description is what a person recognises, the link is
// what gets pasted, so the link is the half that gives way — elided in the
// MIDDLE, because a link's two ends (the repo and the target) are both what
// tells two of them apart.
func (p *linkHistPicker) view(width int) string {
	if !p.loaded {
		return ""
	}
	if len(p.rows) == 0 {
		return st().dim.Render(truncate(i18n.T("no copied links yet"), width))
	}
	descW := 0
	for _, r := range p.rows {
		if w := lipgloss.Width(r.desc); w > descW {
			descW = w
		}
	}
	if max := width * 2 / 5; descW > max {
		descW = max
	}
	linkW := width - descW - 4 // "▸ " + two spaces between the columns
	var b strings.Builder
	for i, r := range p.rows {
		marker := "  "
		if p.active && i == p.sel {
			marker = "▸ "
		}
		line := marker + padRight(truncate(sanitizeRowText(r.desc), descW), descW)
		if linkW > 0 {
			line += "  " + elideMiddle(sanitizeRowText(r.link), linkW)
		}
		if p.active && i == p.sel {
			line = st().selectedRow.Render(line)
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String()
}

// elideMiddle shortens s to at most n display columns by dropping its middle
// around a single "…". Measured in display columns, like truncate.
func elideMiddle(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	r := []rune(s)
	head, tail := 0, len(r)
	// Grow both ends alternately while the whole still fits in n columns.
	for {
		grew := false
		if head < tail && lipgloss.Width(string(r[:head+1]))+1+lipgloss.Width(string(r[tail:])) <= n {
			head++
			grew = true
		}
		if head < tail && lipgloss.Width(string(r[:head]))+1+lipgloss.Width(string(r[tail-1:])) <= n {
			tail--
			grew = true
		}
		if !grew {
			break
		}
	}
	return string(r[:head]) + "…" + string(r[tail:])
}
