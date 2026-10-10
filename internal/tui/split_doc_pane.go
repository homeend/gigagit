package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// docPane is a split pane over an open-file document: the file viewer's
// box, cursor, selection, search, note boxes, bands and keys, in a half of
// the screen. d nil = nothing shown yet (empty is the title then).
type docPane struct {
	d     *openFile
	empty string
	// stackable: S toggles the review split's file pane single ⇄ stacked
	// (the review split's panes only).
	stackable bool
}

func (p *docPane) doc() *openFile { return p.d }

func (p *docPane) title() string {
	if p.d == nil {
		if p.empty != "" {
			return p.empty
		}
		return i18n.T("(no file)")
	}
	return p.d.frameTitle()
}

// captures: the character selection and a search being typed take every
// key (the mode is modal: tab and the split's other keys wait); esc belongs
// to a live line selection (it unmarks) or a committed search (it clears)
// before it closes the split — the viewer's order.
func (p *docPane) captures(msg tea.KeyMsg) bool {
	if p.d == nil {
		return false
	}
	pp := p.d.p
	if pp.cs.on || pp.search.typing {
		return true
	}
	return msg.String() == "esc" && (pp.lsel.on || pp.search.query != "")
}

func (p *docPane) render(m Model, w, h int, focused bool) string {
	if p.d == nil {
		return m.renderPreviewBoxOpts(&contentPopup{}, p.title(), w, h, previewBoxOpts{focused: focused, cursorOff: true})
	}
	return m.renderPreviewBoxOpts(p.d.p, p.title(), w, h, previewBoxOpts{focused: focused, cursorOff: !focused})
}

// hint is the viewer's everyday keys; a file with bands leads with n/p.
func (p *docPane) hint(m Model) string {
	if p.d == nil {
		return ""
	}
	if h, on := paneCharHint(p.d); on {
		return h
	}
	h := i18n.T("[alt+↑↓] line  [spc] mark  [/] find  [X] close  [↑/↓] scroll  [ctrl+w] view")
	if p.stackable {
		h = i18n.T("[N/P] overview") + "  " + i18n.T("[S] stacked") + "  " + h
	}
	if p.d.bands() != nil {
		h = i18n.T("[n/p] anchors") + "  " + h
	}
	return h
}

// update is the file viewer's key order without the keys the split owns
// (esc, backspace, tab): selection, search, note keys, bands, then the
// pager. X closes the document and empties the pane; ctrl+] backgrounds it.
func (p *docPane) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if p.d == nil {
		if msg.String() == "." {
			return m.openActionMenu(), nil
		}
		return m, nil
	}
	if nm, cmd, ok := m.previewCharKey(msg); ok { // modal: before every other key
		return nm, cmd
	}
	if nm, cmd, ok := m.previewSelectKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.previewSearchKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.previewNoteKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.bandKey(msg); ok {
		return nm, cmd
	}
	d := p.d
	pp := d.p
	_, rows, inner, _ := m.activePreview()
	scroll := func(delta int) { pp.scrollBy(delta, rows) }
	switch msg.String() {
	case "S": // single ⇄ stacked, from either pane of the review split
		if sv := m.splitLayer(); p.stackable && sv != nil && m.topLayer() == sv {
			return m.toggleReviewStacked(sv)
		}
	case "X":
		p.d = nil // out of the pane first: closeDoc's detach must not find it here
		return m.closeDoc(d), nil
	case "ctrl+]":
		p.d = nil
		return m.backgroundDoc(d), nil
	case ".":
		return m.openActionMenu(), nil
	case "y":
		if d.result {
			return m, m.copyToClipboardCmd(i18n.T("copied"), docText(d))
		}
	case "alt+up":
		m.movePreviewCursor(-1)
	case "alt+down":
		m.movePreviewCursor(1)
	case "up", "k":
		scroll(-1)
	case "down", "j":
		scroll(1)
	case "pgup":
		scroll(-rows)
	case "pgdown", " ":
		scroll(rows)
	case "home":
		pp.sel = 0
	case "end":
		pp.sel = pp.clampTop(len(pp.lines), rows)
	case "ctrl+w":
		pp.mode = pp.mode.next()
		pp.hscroll = 0
		if d.ov != nil {
			d.layOut(rows, inner)
		}
	case "shift+left":
		if pp.mode == modeScroll && pp.hscroll > 0 {
			if pp.hscroll -= m.hscrollStep(); pp.hscroll < 0 {
				pp.hscroll = 0
			}
		}
	case "shift+right":
		if pp.mode == modeScroll {
			pp.hscroll += m.hscrollStep()
		}
	}
	return m, nil
}

// paneCharHint is the mode's keys while d's character selection is on: the
// split's hint line is then that alone (every other key waits).
func paneCharHint(d *openFile) (string, bool) {
	if d == nil || !d.p.cs.on {
		return "", false
	}
	return charSelHint(d.p.cs, d.p.csRows), true
}
