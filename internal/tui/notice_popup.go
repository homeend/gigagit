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
}

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

// applyNoticeAction removes the notice (acting or dismissing removes it),
// records the dismissal kind, and runs the action's op if it has one.
func (m Model) applyNoticeAction(n notice, act noticeAction) (Model, tea.Cmd) {
	m = m.removeNotice(n.id)
	m.noticeSessionDismissed[n.id] = true // a mid-session health re-read must not resurrect it
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

// box draws the notice dialog (modal box only): the notice list, or (when
// showActions) the selected notice's detail + action list.
func (p *noticePopup) box(m Model) string {
	w, h := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)
	var b strings.Builder
	if p.showActions {
		if n := p.currentNotice(m); n != nil {
			b.WriteString(n.title + "\n\n")
			for _, line := range n.detail {
				if lipgloss.Width(line) <= textW {
					// short lines pass through verbatim — preserves the install
					// table's indentation and column alignment
					b.WriteString(line + "\n")
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
					row = selectedRow.Render("> " + act.label)
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
			for i, n := range m.notices {
				prefix := "  "
				var st lipgloss.Style
				if i == p.sel {
					prefix, st = "> ", selectedRow
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
		b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] actions  [z] mode  [esc] close"))
	}
	return popupBox(inner, strings.TrimRight(b.String(), "\n"))
}
