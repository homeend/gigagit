package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
)

// noticePopup is the ! notification dialog: a list of notices; enter opens
// the selected notice's actions; esc backs out (actions → list → closed).
// Acting or dismissing removes the notice from the session list.
type noticePopup struct {
	popupMax
	sel         int
	showActions bool
	actSel      int
	mode        dispMode
	hscroll     int
	// saved is where `s` last wrote the selected notice. Shown inside the box —
	// the dialog covers the status bar (save_content.go).
	saved string
}

func (p *noticePopup) noteSaved(path string) { p.saved = path }

// openNoticeCenter opens the dialog and marks every notice read (the blink
// tick stops re-arming on its next fire).
func (m Model) openNoticeCenter() (Model, tea.Cmd) {
	m.noticesUnread = false
	return m.pushLayer(&noticePopup{}), nil
}

func (p *noticePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if p.showActions {
			p.showActions = false
			return m, nil
		}
		return m.popLayer(), nil
	}
	if !p.showActions {
		switch msg.String() {
		case "ctrl+w":
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
		case "s": // write the notice to a temp file — see save_content.go for why
			n := p.currentNotice(m)
			if n == nil {
				return m, nil
			}
			return m, saveTextCmd(n.title, append([]string{n.title, ""}, n.detail...))
		}
	}
	switch msg.Type {
	case tea.KeyUp:
		if p.showActions {
			if p.actSel > 0 {
				p.actSel--
			}
		} else if p.sel > 0 {
			p.sel--
		}
	case tea.KeyDown:
		if p.showActions {
			if n := p.currentNotice(m); n != nil && p.actSel < len(n.actions)-1 {
				p.actSel++
			}
		} else if p.sel < len(m.notices)-1 {
			p.sel++
		}
	case tea.KeyEnter:
		if !p.showActions {
			if len(m.notices) == 0 {
				return m, nil
			}
			p.showActions = true
			p.actSel = 0
			return m, nil
		}
		n := p.currentNotice(m)
		if n == nil {
			p.showActions = false
			return m, nil
		}
		if len(n.actions) == 0 {
			p.showActions = false
			return m, nil
		}
		if p.actSel >= len(n.actions) {
			p.actSel = len(n.actions) - 1
		}
		act := n.actions[p.actSel]
		m = m.popLayer() // any action closes the dialog
		return m.applyNoticeAction(*n, act)
	}
	return m, nil // swallow everything else
}

// currentNotice resolves the selected notice against the LIVE list (the
// popup holds indices, not copies).
func (p *noticePopup) currentNotice(m Model) *notice {
	if p.sel < 0 || p.sel >= len(m.notices) {
		return nil
	}
	return &m.notices[p.sel]
}

// applyNoticeAction removes the notice (acting or dismissing removes it —
// unless the action says keep: a copy is not a dismissal), records the
// dismissal kind, and runs the action's op if it has one.
func (m Model) applyNoticeAction(n notice, act noticeAction) (Model, tea.Cmd) {
	if !act.keep {
		m = m.removeNotice(n.id)
		m.noticeSessionDismissed[n.id] = true // a mid-session health re-read must not resurrect it
	}
	if act.never {
		if m.promptStore == nil {
			m.statusMsg = i18n.T("dismissed for this session (no state dir — can't persist)")
		} else if err := m.promptStore.DismissNotice(n.repoKey, n.id); err != nil {
			m.statusMsg = i18n.T("dismissed for this session (couldn't persist: %s)", err.Error())
		} else {
			m.statusMsg = i18n.T("notice dismissed for this repo — %s", defaultPromptStatePath())
		}
	}
	if act.run != nil {
		return act.run(m)
	}
	return m, nil
}

func (p *noticePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// elideNoticeRow fits an indented data row into w columns: everything up to
// its path — the indent and a lead-in such as the drift row's status letter
// ("  A <path>") — stays as written, and the path loses its middle so the
// file name survives (elideRowPath, the elidePath rule). The path is the
// first word holding a path separator; a row without one (a root-level
// file) treats its LAST word as the path, so a bare name still keeps its
// beginning and extension instead of being end-cut. A row that fits is
// untouched.
func elideNoticeRow(line string, w int) string {
	if lipgloss.Width(line) <= w {
		return line
	}
	at := strings.IndexFunc(line, func(r rune) bool { return r == '/' || r == '\\' })
	if at < 0 {
		at = len(strings.TrimRight(line, " \t"))
	}
	head := strings.LastIndexAny(line[:at], " \t") + 1 // the path word starts after the last gap
	return elideRowPath(line, len([]rune(line[:head])), w)
}

// contentWidth is the widest raw line the box is about to show, so the frame
// can follow its content (popupFitWidth) instead of a fixed width: the list's
// titles or the open notice's title, detail lines and actions, plus the key
// hint. Measured before any wrapping — a line wider than the resulting text
// width is what wraps or elides, never what stretches the box past its cap.
func (p *noticePopup) contentWidth(m Model) int {
	widest := 0
	note := func(s string) {
		if w := lipgloss.Width(s); w > widest {
			widest = w
		}
	}
	if p.showActions {
		if n := p.currentNotice(m); n != nil {
			note(n.title)
			for _, line := range n.detail {
				note(line)
			}
			for _, act := range n.actions {
				note("> " + act.label)
			}
		}
		note(i18n.T("[↑/↓] select  [enter] choose  [esc] back"))
		return widest
	}
	note(i18n.T("Notifications"))
	for _, n := range m.notices {
		note("> " + n.title)
	}
	note(i18n.T("[↑/↓] select  [enter] actions  [ctrl+w] mode  [s] save  [esc] close"))
	return widest
}

// box draws the notice dialog (modal box only): the notice list, or (when
// showActions) the selected notice's detail + action list.
func (p *noticePopup) box(m Model) string {
	w, h := m.overlayDims()
	inner := popupFitWidth(w, p.maximized, popupWideInnerWidth(w), p.contentWidth(m))
	textW := popupTextWidth(inner)
	var b strings.Builder
	if p.showActions {
		if n := p.currentNotice(m); n != nil {
			// The title wraps like the prose below it: a drift notice's title
			// carries the branch name, and cutting it with "…" hid the very
			// words that said what happened.
			for _, seg := range wrapWords(n.title, textW) {
				b.WriteString(seg + "\n")
			}
			b.WriteString("\n")
			for _, line := range n.detail {
				if lipgloss.Width(line) <= textW {
					// short lines pass through verbatim — preserves the install
					// table's indentation and column alignment
					b.WriteString(line + "\n")
					continue
				}
				if strings.HasPrefix(line, " ") {
					// An indented line is a raw data row (a flagged path, a
					// lock file), not prose: word-wrapping it dropped the
					// indent, stranded the status letter on its own line and
					// chunked the path mid-name.
					b.WriteString(elideNoticeRow(line, textW) + "\n")
					continue
				}
				for _, seg := range wrapWords(line, textW) {
					b.WriteString(seg + "\n")
				}
			}
			b.WriteString("\n")
			for i, act := range n.actions {
				prefix := "  "
				row := prefix + act.label
				if i == p.actSel {
					row = st().selectedRow.Render("> " + act.label)
				}
				b.WriteString(row + "\n")
			}
			b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] choose  [esc] back"))
		}
	} else {
		b.WriteString(i18n.T("Notifications") + "\n\n")
		if len(m.notices) == 0 {
			b.WriteString("  " + i18n.T("no notices for this repo") + "\n")
		} else {
			wr := make([]winRow, len(m.notices))
			s := st()
			for i, n := range m.notices {
				prefix := "  "
				var st lipgloss.Style
				if i == p.sel {
					prefix, st = "> ", s.selectedRow
				}
				wr[i] = winRow{text: fmt.Sprintf("%s%s", prefix, n.title), style: st}
			}
			rows := len(m.notices)
			capRows := popupResolveRowCap(p.maximized, h, 12)
			if rows > capRows {
				rows = capRows
			}
			for _, line := range renderWindow(wr, winOpts{w: textW, h: rows, mode: p.mode, anchor: p.sel, hscroll: p.hscroll}) {
				b.WriteString(line + "\n")
			}
		}
		if p.saved != "" {
			// Blank line above, then the styled band — see content_popup.go for
			// the truncate-then-style ordering.
			b.WriteString("\n\n" + st().saveBanner.Width(textW).Render(truncate(i18n.T("saved to %s", p.saved), textW)))
		}
		b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] actions  [ctrl+w] mode  [s] save  [esc] close"))
	}
	return popupBox(inner, strings.TrimRight(b.String(), "\n"))
}
