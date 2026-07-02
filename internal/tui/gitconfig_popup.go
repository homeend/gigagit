package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/gitconfdocs"
	"github.com/homeend/gigagit/internal/model"
)

// gitConfigPopup is the Settings → "Git config explorer": every key git
// knows (git help -c), the explicitly-set local/global values, and — for
// curated keys (internal/gitconfdocs) — the real default and a description.
// Navigation-first like the repo switcher: / filters (move-while-typing),
// z cycles display modes, esc closes.
type gitConfigPopup struct {
	rows      []model.GitConfigRow
	loading   bool
	query     string
	filtering bool
	sel       int
	mode      dispMode
	hscroll   int
}

// gitConfigRowsMsg carries the merged rows; gen guards repo switches and
// reopen races.
type gitConfigRowsMsg struct {
	gen  int
	rows []model.GitConfigRow
	err  error
}

// openGitConfigExplorer pushes the loading popup and reads the rows off the
// UI thread.
func (m Model) openGitConfigExplorer() (Model, tea.Cmd) {
	m.gitConfigGen++
	m = m.pushLayer(&gitConfigPopup{loading: true})
	svc := m.svc
	gen := m.gitConfigGen
	return m, func() tea.Msg {
		rows, err := svc.GitConfigRows(context.Background())
		return gitConfigRowsMsg{gen: gen, rows: rows, err: err}
	}
}

// moveSel moves the cursor by d, clamped to the filtered view.
func (p *gitConfigPopup) moveSel(d int) {
	n := p.sel + d
	if hi := len(p.visible()) - 1; n > hi {
		n = hi
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

// visible returns the filtered rows (case-insensitive substring over the key
// and both values).
func (p *gitConfigPopup) visible() []model.GitConfigRow {
	if p.query == "" {
		return p.rows
	}
	q := strings.ToLower(p.query)
	out := make([]model.GitConfigRow, 0, len(p.rows))
	for _, r := range p.rows {
		if strings.Contains(strings.ToLower(r.Key), q) ||
			strings.Contains(strings.ToLower(r.LocalValue), q) ||
			strings.Contains(strings.ToLower(r.GlobalValue), q) {
			out = append(out, r)
		}
	}
	return out
}

// update handles all keys while the explorer is open. It swallows everything
// (no fallthrough to global handlers), mirroring repoPopup: plain keys
// navigate, `/` enters a filter sub-mode where runes (including `z`) type a
// query until esc/enter. (Task 6 adds l/g/u edit keys here.)
func (p *gitConfigPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
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

// render composites the explorer over the layer beneath.
func (p *gitConfigPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// Column-width tuning: the ideal split is key 36 | local 18 | global 18 |
// default (rest). On a narrower terminal (the default 80-col test terminal
// included) that ideal sum doesn't fit the popup's text width, so key gives
// way first (a shortened key is still identifiable, and z switches to
// wrap/scroll to see the rest), then local/global shrink together; the
// default column — usually a short literal like "false" or "—" — always
// keeps a small floor so it never disappears entirely.
const (
	gitConfigKeyIdeal    = 36
	gitConfigLocalIdeal  = 18
	gitConfigGlobalIdeal = 18
	gitConfigColSep      = 1
	gitConfigKeyFloor    = 22
	gitConfigSideFloor   = 8
	gitConfigDefaultMin  = 4
)

// gitConfigColWidths computes the four column widths for the given text
// width, shrinking from the ideal split when necessary (see the const block
// above for the shrink order and rationale).
func gitConfigColWidths(textW int) (keyW, localW, globalW, defaultW int) {
	keyW, localW, globalW = gitConfigKeyIdeal, gitConfigLocalIdeal, gitConfigGlobalIdeal
	sep := 3 * gitConfigColSep
	need := func() int { return keyW + localW + globalW + sep + gitConfigDefaultMin }
	for need() > textW && keyW > gitConfigKeyFloor {
		keyW--
	}
	for need() > textW && localW > gitConfigSideFloor {
		localW--
	}
	for need() > textW && globalW > gitConfigSideFloor {
		globalW--
	}
	defaultW = textW - keyW - localW - globalW - sep
	if defaultW < 1 {
		defaultW = 1
	}
	return
}

// gitConfigDefaultCell renders the default-value cell: the curated default
// for a known key, or an em dash for one gg doesn't curate.
func gitConfigDefaultCell(key string, width int) string {
	def := "—"
	if doc := gitconfdocs.Lookup(key); doc != nil {
		def = doc.Default
	}
	return padRight(truncate(def, width), width)
}

// gitConfigRowText lays out one row's four columns at the given widths.
func gitConfigRowText(r model.GitConfigRow, keyW, localW, globalW, defaultW int) string {
	key := padRight(truncate(r.Key, keyW), keyW)
	local := configCell(r.LocalValue, r.LocalSet, localW)
	global := configCell(r.GlobalValue, r.GlobalSet, globalW)
	def := gitConfigDefaultCell(r.Key, defaultW)
	return key + " " + local + " " + global + " " + def
}

// box draws the explorer box (modal box only).
func (p *gitConfigPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupWideInnerWidth(w)
	textW := popupTextWidth(inner)

	title := "Git config"
	if p.loading {
		title += " (⏳ loading…)"
	} else {
		title += fmt.Sprintf(" (%d keys)", len(p.rows))
	}
	switch {
	case p.filtering:
		title += "  /" + p.query + "█"
	case p.query != "":
		title += "  /" + p.query
	default:
		title += "   (press / to filter)"
	}

	keyW, localW, globalW, defaultW := gitConfigColWidths(textW)
	header := padRight("Key", keyW) + " " + padRight("Local", localW) + " " + padRight("Global", globalW) + " " + padRight("Default", defaultW)

	vis := p.visible()
	var bodyLines []string
	switch {
	case p.loading:
		bodyLines = []string{padRight("  loading…", textW)}
	case len(vis) == 0:
		bodyLines = []string{padRight("  (no match)", textW)}
	default:
		wr := make([]winRow, len(vis))
		for i, r := range vis {
			var st lipgloss.Style
			if i == p.sel {
				st = selectedRow
			}
			wr[i] = winRow{text: gitConfigRowText(r, keyW, localW, globalW, defaultW), style: st}
		}
		// Height budget: capped like the session-errors viewer so the popup
		// stays on-screen no matter how many keys git knows about.
		capRows := termH - 12
		if capRows < 3 {
			capRows = 3
		}
		h := len(vis)
		if h > capRows {
			h = capRows
		}
		bodyLines = renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	}

	// The selected row's curated description (blank for a non-curated key).
	// Wrapped (not hard-truncated) so a long description — up to a few
	// lines — stays fully readable instead of losing its tail to "…".
	var descLine string
	if p.sel >= 0 && p.sel < len(vis) {
		if doc := gitconfdocs.Lookup(vis[p.sel].Key); doc != nil {
			descLine = doc.Desc
		}
	}
	descLines := []string{""}
	if descLine != "" {
		descLines = wrapWidth(descLine, textW, 3)
	}

	hint := []string{"[/] filter", "[z] mode", "[esc] close"}
	parts := []string{title, "", header}
	parts = append(parts, bodyLines...)
	parts = append(parts, "")
	parts = append(parts, descLines...)
	parts = append(parts, "")
	parts = append(parts, wrapParts(hint, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// configCell renders one scope cell: the value, or a dim "(unset)".
func configCell(v string, set bool, width int) string {
	if !set {
		return unsetStyle.Render(padRight(truncate("(unset)", width), width))
	}
	return padRight(truncate(v, width), width)
}

var unsetStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
