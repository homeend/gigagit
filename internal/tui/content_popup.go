package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
)

// contentLine is one display line of a contentPopup. heading lines are section
// headers: the filter never matches them, and they survive filtering only
// while at least one non-heading line beneath them (before the next heading)
// matches.
//
// path/oldPath/status are an optional file payload (the commit files tree):
// the diff loader reads them straight off the selected VISIBLE line —
// payload-on-line is the only shape that survives the /-filter, whose
// visible() returns a reordered subset. Zero-valued for headings and for
// consumers that don't need them (the help window).
type contentLine struct {
	text    string
	heading bool
	path    string // file's (new) path
	oldPath string // set only for renames/copies
	status  string // model.CommitFile.Status letter ("A","M","D","R","C","T")
	sha     string // per-line commit override (a -u stash's ^3 parent); "" = the view's hash
}

// contentPopup is a generic read-only viewer popup: any list of lines with
// repo-popup-style type-to-filter search and cursor-driven scrolling. The
// help window is its first consumer.
type contentPopup struct {
	popupMax
	title   string
	lines   []contentLine // full, unfiltered content
	query   string        // case-insensitive substring over non-heading lines
	typing  bool          // true while /-input mode is capturing keys
	sel     int           // cursor index into the FILTERED view
	mode    dispMode      // text display mode; z cycles
	hscroll int           // modeScroll horizontal offset
	footer  string        // optional line above the hint (e.g. commit author · date); "" = none
	danger  bool          // frame the box in red — a failure, not information (error_popup.go)
	// noCursor drops the row cursor: no "> " marker and no reverse-video
	// highlight. A viewer showing one prose message has nothing to select or
	// act on, so the cursor is noise — the rows are text, not choices.
	noCursor bool
	// block lays the content out as one quoted message rather than a list of
	// rows: a faint background band spanning the box, every line (including a
	// wrap continuation) on the same margin as the title and the key hints, and
	// a blank line below it so the hints read as actions rather than as one
	// more line of git's stderr. Set by the [E] viewer (error_popup.go).
	block bool
	// saved is where `s` last wrote this window's text. Shown inside the box:
	// a tall popup covers the status bar, so reporting the path only there
	// hides it behind the box's own border (save_content.go).
	saved string
}

// The message a block-mode viewer shows is quoted text — git's stderr, a
// notice's fix instructions — not the popup's own words. A faint band behind
// it says so, the way a code block does in prose: dark enough to separate it
// from the box, light enough to leave the text at the terminal's own
// foreground (the box can already be framed red; red-on-red would not read).
// The gutter is the band's inner padding, and matches the indent the title and
// the key hints take, so every line in the box starts in one column.
const (
	messageBlockColor  = "236"
	messageBlockGutter = 2
)

var messageBlockStyle = lipgloss.NewStyle().Background(lipgloss.Color(messageBlockColor))

func (p *contentPopup) noteSaved(path string) { p.saved = path }

func newContentPopup(title string, lines []contentLine) *contentPopup {
	return &contentPopup{title: title, lines: lines}
}

// isLoadingPlaceholder reports whether text is the (possibly translated)
// "(loading…)" placeholder written by the async-load construction sites.
// Translation is resolved live against the ACTIVE catalog (i18n.T is
// process-global) so a completion handler can recognize its own placeholder
// under any language, not just English — comparing against the raw English
// literal silently breaks the failure path once the placeholder is translated.
func isLoadingPlaceholder(text string) bool {
	return text == i18n.T("(loading…)")
}

// visible returns the filtered lines in display order: non-heading lines
// matching query, plus each heading that still has a matching line.
func (p *contentPopup) visible() []contentLine {
	if p.query == "" {
		return p.lines
	}
	q := strings.ToLower(p.query)
	out := make([]contentLine, 0, len(p.lines))
	var pending *contentLine // last heading not yet emitted
	for i := range p.lines {
		l := p.lines[i]
		if l.heading {
			pending = &p.lines[i]
			continue
		}
		if strings.Contains(strings.ToLower(l.text), q) {
			if pending != nil {
				out = append(out, *pending)
				pending = nil
			}
			out = append(out, l)
		}
	}
	return out
}

// move shifts the cursor by delta, clamped to the visible range.
func (p *contentPopup) move(delta int) {
	n := len(p.visible())
	p.sel += delta
	if p.sel > n-1 {
		p.sel = n - 1
	}
	if p.sel < 0 {
		p.sel = 0
	}
}

// contentFastStep is the ctrl+↑/↓ jump (the mouse-wheel tick is the
// configurable wheelStep).
const contentFastStep = 5

// contentPageRows is the popup's visible row capacity: terminal height minus
// chrome (double border 2, vertical padding 2, title 1, blank after title 1,
// hint line 1), floored so tiny terminals still show a few rows.
func (m Model) contentPageRows() int {
	_, h := m.overlayDims()
	n := h - 7
	if n < 3 {
		n = 3
	}
	return n
}

// update handles all keys while the viewer is open. It swallows everything (no
// fallthrough to global handlers). Search mirrors the panel filter: / starts
// input mode, enter keeps the query, esc cancels it.
func (p *contentPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.typing { // /-input mode captures every key
		if m.filesView == p { // only the files-view tree search has history recall
			if nm, nq, handled, commit := m.recallUpdate(scopeFiletree, msg, p.query); handled {
				m = nm
				p.query = nq
				p.sel = 0
				if commit {
					p.typing = false
					return m.recordSearch(scopeFiletree, p.query)
				}
				return m, nil
			} else {
				m = nm
			}
		}
		// Arrows/pages move the selection live while typing (no cursor reset),
		// like the commit filter; j/k stay query text.
		if filterMotion(msg, p.move, m.contentPageRows()) {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEsc:
			p.typing = false
			p.query = ""
			p.sel = 0
		case tea.KeyEnter:
			p.typing = false      // commit: search stays active
			if m.filesView == p { // only the files-view tree search has history
				return m.recordSearch(scopeFiletree, p.query)
			}
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
	switch msg.String() {
	case "z": // cycle the text display mode (cutoff / wrap / scroll)
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
	case "s": // write the text to a temp file — see save_content.go for why
		lines := make([]string, 0, len(p.lines))
		for _, l := range p.lines {
			lines = append(lines, l.text)
		}
		return m, saveTextCmd(p.title, lines)
	case "q": // close the window, not the app (q quits only at top level)
		m = m.popLayer()
		return m, nil
	case "esc":
		if p.query != "" { // first esc clears the committed search, second closes
			p.query = ""
			p.sel = 0
			return m, nil
		}
		m = m.popLayer()
		return m, nil
	case "enter":
		m = m.popLayer()
		return m, nil
	case "/":
		p.typing = true
		p.query = ""
		p.sel = 0
		m = m.recallReset()
	case "up", "k":
		p.move(-1)
	case "down", "j":
		p.move(1)
	case "ctrl+up":
		p.move(-contentFastStep)
	case "ctrl+down":
		p.move(contentFastStep)
	case "pgup":
		p.move(-m.contentPageRows())
	case "pgdown":
		p.move(m.contentPageRows())
	}
	return m, nil
}

// contentPopupWidth is the viewer's box width: wider than the standard
// 56-column form popup (popupInnerWidth) because it shows a two-column
// reference table — most of the terminal, capped at 100 columns.
func contentPopupWidth(w int) int {
	inner := 100
	if max := w - 8; inner > max {
		inner = max
	}
	if inner < 20 {
		inner = 20
	}
	return inner
}

// searchLine is the /-search input shown on its OWN line beneath a title:
// "/<query>█" while typing, "/<query>" for a committed query, or "" when no
// search is active. Kept off the title line so a long title (e.g. a long commit
// subject) can't truncate the query out of view. Shared by every surface that
// renders a contentPopup search (the popup itself and the files-view tree).
func (p *contentPopup) searchLine() string {
	switch {
	case p.typing:
		return "/" + p.query + "█"
	case p.query != "":
		return "/" + p.query
	default:
		return ""
	}
}

// render composites the viewer over the layer beneath.
func (p *contentPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the viewer box. Headings render bold, the cursor row reversed; the
// window follows the cursor via the same windowRows helper the panels use.
func (p *contentPopup) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, contentPopupWidth(w))
	// lipgloss wraps text at Width minus the horizontal padding; truncate to
	// that true text width so a full-width row can never spill onto a wrap line.
	textW := inner - modalStyle.GetHorizontalPadding()

	// In block mode the indent is a frozen prefix column, not part of the row
	// text: that is what keeps a WRAPPED continuation on the same margin as the
	// line it belongs to (baked-in spaces are consumed by the first display
	// line only). The matching suffix keeps the band's right margin equal.
	gutter := 0
	if p.block {
		gutter = messageBlockGutter
	}
	pad := strings.Repeat(" ", gutter)
	bodyW := textW - 2*gutter

	vis := p.visible()
	wr := make([]winRow, len(vis))
	for i, l := range vis {
		switch {
		case p.block:
			wr[i] = winRow{text: l.text, style: messageBlockStyle}
			if l.heading {
				wr[i].style = messageBlockStyle.Bold(true)
			}
		case p.noCursor:
			wr[i] = winRow{text: "  " + l.text}
			if l.heading {
				wr[i].style = titleStyle
			}
		case i == p.sel:
			// Cursor highlight wins over heading style: the cursor must remain
			// visible even when it rests on a heading row.
			wr[i] = winRow{text: "> " + l.text, style: selectedRow}
		case l.heading:
			wr[i] = winRow{text: l.text, style: titleStyle}
		default:
			wr[i] = winRow{text: "  " + l.text}
		}
	}
	capRows := m.contentPageRows()
	// contentPageRows budgets for title + blank + hint. Block mode draws one
	// more chrome line (the blank below the band), and a search line adds
	// another — without paying for them the box grows past the terminal.
	if p.block {
		extra := 1
		if p.searchLine() != "" {
			extra++
		}
		if capRows-extra >= 3 {
			capRows -= extra
		}
	}
	// h grows with content up to the page capacity; renderWindow scrolls to keep
	// p.sel visible once vis overflows. Styling is applied after truncate/wrap.
	//
	// renderWindow's h is a budget of DISPLAY lines, not of rows. In the
	// single-line modes those coincide, but a wrapped row spans several lines,
	// so sizing by len(vis) would clip the wrap — a one-line error message
	// would render only its first line, which is exactly what the [E] viewer
	// exists to avoid. Measure with the same wrapWidth renderWindow itself
	// uses so the count cannot drift from the layout it is sizing.
	h := len(vis)
	if p.mode == modeWrap {
		h = 0
		for _, r := range wr {
			// An empty row wraps to NO segments but still occupies one display
			// line (renderWindow substitutes a blank), so counting the segments
			// alone loses a line per blank — and git's stderr separates its
			// paragraphs with blank lines, so the tail fell off the window.
			n := len(wrapWidth(r.text, bodyW, 1<<20))
			if n == 0 {
				n = 1
			}
			h += n
		}
	}
	if h > capRows {
		h = capRows
	}
	win := renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll, prefixW: gutter, suffixW: gutter})

	var b strings.Builder
	// The /-search input rides its own line beneath the title (replacing the
	// blank separator) so a long title can't truncate the query out of view.
	// Block mode keeps BOTH — the band needs its blank line above it as much as
	// the one below it, or the message runs into the search input.
	b.WriteString(pad + truncate(p.title, textW-gutter) + "\n")
	if s := p.searchLine(); s != "" {
		b.WriteString(pad + truncate(s, textW-gutter) + "\n")
	}
	if s := p.searchLine(); s == "" || p.block {
		b.WriteString("\n")
	}
	if len(vis) == 0 {
		b.WriteString(pad + i18n.T("  (no match)") + "\n")
	}
	for _, r := range win {
		b.WriteString(r + "\n")
	}
	if p.block {
		b.WriteString("\n") // set the actions apart from the message itself
	}
	if p.footer != "" {
		b.WriteString("  " + truncate(p.footer, textW-2) + "\n")
	}
	if p.saved != "" {
		// Style AFTER truncation: truncate slices runes and would cut an ANSI
		// sequence in half (the same rule the status bar follows).
		b.WriteString("\n  " + savedNoteStyle.Width(textW-2).Render(truncate(i18n.T("saved to %s", p.saved), textW-2)) + "\n")
	}
	hint := i18n.T("[/] search  [z] mode  [s] save  [ctrl+t] full  [q] close")
	if len(vis) > capRows {
		hint = fmt.Sprintf("%d/%d  %s", p.sel+1, len(vis), hint)
	}
	b.WriteString(pad + truncate(hint, textW-gutter))
	return p.boxStyle().Width(inner).Render(b.String()) + "\n"
}

// boxStyle picks the frame: the ordinary yellow modal border, or a red one
// when the popup reports a failure. Only the border colour changes, so the
// body text keeps the terminal's own foreground and stays readable — a
// red-on-red block of wrapped stderr would not.
func (p *contentPopup) boxStyle() lipgloss.Style {
	if p.danger {
		return modalStyle.BorderForeground(lipgloss.Color("9"))
	}
	return modalStyle
}
