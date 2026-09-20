package tui

import (
	"context"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// prSearchPopup asks the forge for pull requests of ANY state — the way to a
// closed or merged PR gg never fetched. Two zones share the keyboard: the
// prompt (text + the state chip) and the result rows, which act like rows of
// the Pull requests tab (enter opens the diff, i the details, y copies the
// URL). The results are a transient set: nothing here joins the tab's list
// until a PR is opened, whose fetched ref then makes it known. Read-only, like
// everything forge.
type prSearchPopup struct {
	popupMax
	input textfield
	state string         // a domain.PRStateFilters() value
	asked domain.PRQuery // the query in flight, or the one res answers
	busy  bool
	err   string
	res   domain.PRSearchResult
	has   bool // res holds an answer (possibly an empty one)

	inRows  bool // the rows zone has the keyboard
	sel     int
	mode    dispMode
	hscroll int
}

// prSearchMsg is one answered search; q gates a stale answer.
type prSearchMsg struct {
	q   domain.PRQuery
	res domain.PRSearchResult
	err error
}

// canSearchPRs gates A: the Pull requests tab focused (so a usable forge) and
// no op in the way. It needs no row — an empty list is when search matters.
func (m Model) canSearchPRs() bool {
	return m.focus == panelPRs && m.forgeShown && m.svc != nil && m.opsIdle()
}

// openPRSearch pushes the popup showing the session's last search at once —
// query, state and rows — without a forge call (stale-first).
func (m Model) openPRSearch() (Model, tea.Cmd) {
	if !m.canSearchPRs() {
		return m, nil
	}
	p := &prSearchPopup{input: newTextField(""), state: domain.PRStateFilterAll}
	if last, ok := m.svc.PRSearchLast(); ok {
		p.input, p.state = newTextField(last.Query.Text), last.Query.State
		p.asked, p.res, p.has = last.Query, last, true
	}
	return m.pushLayer(p), nil
}

func (m Model) prSearchCmd(q domain.PRQuery) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		res, err := svc.PRSearch(context.Background(), q)
		return prSearchMsg{q: q, res: res, err: err}
	}
}

// handlePRSearchMsg lands an answer in the popup it was asked from; an answer
// to an older query (the user searched again) or to a closed popup is dropped.
// A failure keeps the rows already on screen.
func (m Model) handlePRSearchMsg(msg prSearchMsg) (Model, tea.Cmd) {
	p := layerOf[*prSearchPopup](m)
	if p == nil || !p.busy || msg.q != p.asked {
		return m, nil
	}
	p.busy = false
	if msg.err != nil {
		p.err = firstLine(errText(msg.err))
		return m, nil
	}
	p.err, p.res, p.has = "", msg.res, true
	p.sel, p.hscroll = 0, 0
	p.inRows = len(msg.res.PRs) > 0
	return m, nil
}

func (p *prSearchPopup) selected() (model.PullRequest, bool) {
	if !p.has || p.sel < 0 || p.sel >= len(p.res.PRs) {
		return model.PullRequest{}, false
	}
	return p.res.PRs[p.sel], true
}

func (p *prSearchPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.inRows {
		return p.updateRows(m, msg)
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyTab:
		states := domain.PRStateFilters()
		p.state = states[(slices.Index(states, p.state)+1)%len(states)]
	case tea.KeyDown:
		if p.has && len(p.res.PRs) > 0 {
			p.inRows = true
		}
	case tea.KeyEnter:
		if m.svc == nil {
			return m, nil
		}
		q, err := domain.NormalizePRQuery(domain.PRQuery{State: p.state, Text: p.input.Value()})
		if err != nil {
			p.err = err.Error()
			return m, nil
		}
		p.asked, p.busy, p.err = q, true, ""
		return m, m.prSearchCmd(q)
	default:
		p.input.HandleEditKey(msg)
	}
	return m, nil
}

func (p *prSearchPopup) updateRows(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	n := len(p.res.PRs)
	page := 10
	switch msg.String() {
	case "esc":
		p.inRows = false
	case "up", "k":
		if p.sel == 0 {
			p.inRows = false
		} else {
			p.sel--
		}
	case "down", "j":
		if p.sel < n-1 {
			p.sel++
		}
	case "pgup":
		p.sel = max(0, p.sel-page)
	case "pgdown":
		p.sel = min(n-1, p.sel+page)
	case "home":
		p.sel = 0
	case "end":
		p.sel = n - 1
	case "ctrl+w":
		p.mode, p.hscroll = p.mode.next(), 0
	case "shift+left":
		if p.mode == modeScroll {
			p.hscroll = max(0, p.hscroll-m.hscrollStep())
		}
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
	case "enter":
		// The popup stays on the stack through the fetch; the PR's diff parks
		// it (handlePreviewOpenMsg), so closing the diff comes back here.
		if pr, ok := p.selected(); ok && m.opsIdle() {
			// The fetched ref makes a found PR KNOWN: re-read the tab's list
			// once the fetch lands, so it is there when the popup closes.
			m.pendingPRsReload = !slices.ContainsFunc(m.prs, func(have model.PullRequest) bool { return have.Number == pr.Number })
			return m.openPRCmd(pr)
		}
	case "i":
		if pr, ok := p.selected(); ok {
			return m.openPRHub(pr)
		}
	case "y":
		if pr, ok := p.selected(); ok {
			if pr.URL == "" {
				m.statusMsg = i18n.T("this pull request has no URL")
				return m, nil
			}
			return m, m.copyToClipboardCmd(i18n.T("copied PR URL"), pr.URL)
		}
	}
	return m, nil
}

func (p *prSearchPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// prSearchStateWord is the state chip's text.
func prSearchStateWord(state string) string {
	switch state {
	case domain.PRStateFilterOpen:
		return i18n.T("open")
	case domain.PRStateFilterClosed:
		return i18n.T("closed")
	case domain.PRStateFilterMerged:
		return i18n.T("merged")
	}
	return i18n.T("all")
}

func (p *prSearchPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupFullInnerWidth(w)
	textW := popupTextWidth(inner)
	s := st()

	chip := "[" + prSearchStateWord(p.state) + "] "
	parts := []string{
		i18n.T("Search pull requests"),
		"",
		viewField(chip, p.input, !p.inRows, textW),
		strings.Repeat("─", textW),
	}
	switch {
	case p.busy:
		parts = append(parts, s.dim.Render(i18n.T("searching…")))
	case p.err != "":
		parts = append(parts, s.errorText.Render(truncate(p.err, textW)))
	}
	switch {
	case !p.has:
		if !p.busy && p.err == "" {
			// Packed word by word: the hint must read whole in a narrow popup.
			intro := i18n.T("closed, merged and open pull requests — text goes to the forge's search; a number opens that pull request")
			for _, l := range wrapParts(strings.Fields(intro), textW, " ") {
				parts = append(parts, s.dim.Render(l))
			}
		}
	case len(p.res.PRs) == 0:
		parts = append(parts, s.dim.Render(i18n.T("no pull requests match")))
	default:
		text := prRowsFor(p.res.PRs)
		rows := make([]winRow, len(text))
		for i, t := range text {
			prefix := "  "
			if p.inRows && i == p.sel {
				prefix = "> "
			}
			r := winRow{text: prefix + t}
			switch {
			case p.inRows && i == p.sel:
				r.style = s.selectedRow
			case !p.res.PRs[i].IsOpen():
				r.style = s.dim
			}
			rows[i] = r
		}
		// The box must fit the screen: the fixed lines around the rows are
		// the title, blank, prompt, rule, a status line, "more", blank and
		// the hint (which may wrap to two).
		capRows := popupResolveRowCap(p.maxed(), termH, 12)
		if room := termH - 14; capRows > room {
			capRows = max(3, room)
		}
		o := winOpts{w: textW, mode: p.mode, anchor: p.sel, hscroll: p.hscroll}
		o.h = wrapContentLines(rows, o, capRows)
		parts = append(parts, renderWindow(rows, o)...)
		if p.res.More {
			parts = append(parts, s.dim.Render(truncate(i18n.T("more results — narrow the search"), textW)))
		}
	}
	hint := []string{i18n.T("[enter] search"), i18n.T("[tab] state"), i18n.T("[↓] results"), i18n.T("[esc] close")}
	if p.inRows {
		hint = []string{i18n.T("[enter] open"), i18n.T("[i]nfo"), i18n.T("[y] copy URL"), i18n.T("[ctrl+w] mode"), i18n.T("[esc] back")}
	}
	parts = append(parts, "")
	parts = append(parts, wrapParts(hint, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}
