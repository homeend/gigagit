package tui

import (
	"context"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// The send panel (spec §5.3, R6, R7): every unsent local comment of a pull
// request — each AI review's remarks, my notes, my draft replies — in one
// list, nothing ticked on open. The user ticks any mix, picks a body, and
// ctrl+s hands ONE PRSendRequest{Notes, Verdict} to forgeSendCmd: the
// existing confirm (comment / approve / request changes), progress and
// outcome. The panel stays under the confirm; forgeSendFinished removes it
// once something changed (A7).

// panelRow is one display row: a group header or a candidate.
type panelRow struct {
	group int // index into cands.Groups
	cand  int // index into the group's Rows; -1 = the header
}

type sendPanel struct {
	popupMax
	pr     int
	cands  domain.SendCandidates
	rows   []panelRow
	sel    int
	ticked map[string]bool
	// body: bodyNone, bodyReview (bodyFrom = the review id) or bodyTyped.
	body     int
	bodyFrom string
	typed    string
	code     bool   // c: the code excerpt under the current row
	gen      int    // m.forgeGen when opened: a repository switch makes the panel stale
	notice   string // the bottom bar: a refused tick, "tick something to send"
}

const (
	bodyNone = iota
	bodyReview
	bodyTyped
)

// sendPanelMsg is PRSendCandidates' answer.
type sendPanelMsg struct {
	gen   int // m.forgeGen when asked
	pr    int
	cands domain.SendCandidates
	err   error
}

// openSendPanel reads PR pr's candidates off the UI thread.
func (m Model) openSendPanel(pr int) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil || pr == 0 {
		return m, nil
	}
	gen := m.forgeGen
	return m, func() tea.Msg {
		c, err := svc.PRSendCandidates(context.Background(), pr)
		return sendPanelMsg{gen: gen, pr: pr, cands: c, err: err}
	}
}

// handleSendPanel opens the panel, or says why there is none.
func (m Model) handleSendPanel(msg sendPanelMsg) (Model, tea.Cmd) {
	if msg.gen != m.forgeGen {
		return m, nil // asked in the repository before R
	}
	if m.modal != nil {
		return m.sendDialogBusy(), nil
	}
	if msg.err != nil {
		return m.sayInDiff(i18n.T("send: %s", firstLine(msg.err.Error()))), nil
	}
	if len(msg.cands.Groups) == 0 {
		return m.sayInDiff(i18n.T("nothing to send to #%d", msg.pr)), nil
	}
	if old := layerOf[*sendPanel](m); old != nil {
		m = m.removeLayer(old)
	}
	p := &sendPanel{pr: msg.pr, cands: msg.cands, ticked: map[string]bool{}, code: true, gen: m.forgeGen}
	sortPanelGroups(p.cands.Groups)
	for g := range p.cands.Groups {
		p.rows = append(p.rows, panelRow{group: g, cand: -1})
		for c := range p.cands.Groups[g].Rows {
			p.rows = append(p.rows, panelRow{group: g, cand: c})
		}
	}
	p.sel = p.firstCandidate()
	if kept, ok := m.keptBodyFor(msg.pr, sendGroupPanel, false); ok {
		p.typed = kept // what the user typed last time survives an abort or a failure
		if p.typed != "" {
			p.body = bodyTyped // …and stays the body: a quick ctrl+s must not drop it
		}
	}
	return m.pushLayer(p), nil
}

// sortPanelGroups keeps the domain's order (reviews newest first, mine,
// replies) and breaks a tie between two reviews created in the same
// second by id, so the list is stable between opens.
func sortPanelGroups(gs []domain.SendCandidateGroup) {
	sort.SliceStable(gs, func(i, j int) bool {
		a, b := gs[i], gs[j]
		if a.Kind != "review" || b.Kind != "review" || !a.Created.Equal(b.Created) {
			return false
		}
		return a.ID < b.ID
	})
}

func (p *sendPanel) firstCandidate() int {
	for i, r := range p.rows {
		if r.cand >= 0 {
			return i
		}
	}
	return 0
}

// candAt is row i's group and, for a candidate row, the candidate (nil on
// a header).
func (p *sendPanel) candAt(i int) (domain.SendCandidateGroup, *domain.SendCandidate, bool) {
	if i < 0 || i >= len(p.rows) {
		return domain.SendCandidateGroup{}, nil, false
	}
	r := p.rows[i]
	g := p.cands.Groups[r.group]
	if r.cand < 0 {
		return g, nil, true
	}
	return g, &g.Rows[r.cand], true
}

func (p *sendPanel) tickedCount() int { return len(p.tickedIDs()) }

// tickedIDs are the ticked candidates in list order: the request's Notes.
func (p *sendPanel) tickedIDs() []string {
	var ids []string
	for _, r := range p.rows {
		if r.cand >= 0 {
			if id := p.cands.Groups[r.group].Rows[r.cand].ID; p.ticked[id] {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// tickedReviews are the AI reviews with a ticked remark, newest first: the
// body cycle's review choices.
func (p *sendPanel) tickedReviews() []domain.SendCandidateGroup {
	var out []domain.SendCandidateGroup
	for _, g := range p.cands.Groups {
		if g.Kind != "review" {
			continue
		}
		for _, c := range g.Rows {
			if p.ticked[c.ID] {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func reviewIDOf(g domain.SendCandidateGroup) string { return strings.TrimPrefix(g.ID, "review:") }

// settleBody drops a review body whose review is no longer ticked.
func (p *sendPanel) settleBody() {
	if p.body != bodyReview {
		return
	}
	for _, g := range p.tickedReviews() {
		if reviewIDOf(g) == p.bodyFrom {
			return
		}
	}
	p.body, p.bodyFrom = bodyNone, ""
}

// cycleBody is b: none → each ticked AI review (newest first) → typed → none.
func (p *sendPanel) cycleBody() {
	rs := p.tickedReviews()
	switch p.body {
	case bodyNone:
		if len(rs) > 0 {
			p.body, p.bodyFrom = bodyReview, reviewIDOf(rs[0])
			return
		}
		p.body = bodyTyped
	case bodyReview:
		for i, g := range rs {
			if reviewIDOf(g) == p.bodyFrom && i+1 < len(rs) {
				p.bodyFrom = reviewIDOf(rs[i+1])
				return
			}
		}
		p.body, p.bodyFrom = bodyTyped, ""
	default:
		p.body = bodyNone
	}
}

// bodyLabel is the Body row's word: none / review text (<agent> · <time>) / typed.
func (p *sendPanel) bodyLabel() string {
	switch p.body {
	case bodyReview:
		for _, g := range p.cands.Groups {
			if g.Kind == "review" && reviewIDOf(g) == p.bodyFrom {
				return i18n.T("review text (%s)", g.Agent+" · "+g.Created.Local().Format("15:04"))
			}
		}
		return i18n.T("none")
	case bodyTyped:
		return i18n.T("typed")
	}
	return i18n.T("none")
}

// request is what ctrl+s sends; false with nothing ticked.
func (p *sendPanel) request() (domain.PRSendRequest, bool) {
	ids := p.tickedIDs()
	if len(ids) == 0 {
		return domain.PRSendRequest{}, false
	}
	req := domain.PRSendRequest{PR: p.pr, Notes: ids, Verdict: true}
	switch p.body {
	case bodyReview:
		req.BodyFrom = p.bodyFrom
	case bodyTyped:
		req.Body, req.BodySet = strings.TrimSpace(p.typed), true
	}
	return req, true
}

func (p *sendPanel) move(d int) {
	n := len(p.rows)
	if n == 0 {
		return
	}
	p.sel = max(0, min(n-1, p.sel+d))
	p.notice = ""
}

// tick is space: the row under the cursor (a skip row says why instead; a
// header ticks its group).
func (p *sendPanel) tick() {
	_, c, ok := p.candAt(p.sel)
	if !ok || c == nil {
		p.tickAll()
		return
	}
	if c.Skip != "" {
		p.notice = strings.TrimSpace(sendSkipReasonText(sendWhere(c.Path, c.Range[0])+" "+sanitizeLine(c.Summary), c.Skip))
		return
	}
	if p.ticked[c.ID] {
		delete(p.ticked, c.ID)
	} else {
		p.ticked[c.ID] = true
	}
	p.settleBody()
}

// tickAll is a: every sendable row of the cursor's group on, or — when they
// all are — off.
func (p *sendPanel) tickAll() {
	g, _, ok := p.candAt(p.sel)
	if !ok {
		return
	}
	all := true
	for _, c := range g.Rows {
		if c.Skip == "" && !p.ticked[c.ID] {
			all = false
		}
	}
	for _, c := range g.Rows {
		if c.Skip != "" {
			continue
		}
		if all {
			delete(p.ticked, c.ID)
		} else {
			p.ticked[c.ID] = true
		}
	}
	p.settleBody()
}

// openRow is enter: the row's file in the PR diff, the cursor on its thread,
// the diff above the panel (esc returns to it, A6). Only while the PR's own
// file list is open — the panel also opens from the PR tab's details.
func (p *sendPanel) openRow(m Model) (Model, tea.Cmd) {
	_, c, ok := p.candAt(p.sel)
	if !ok || c == nil {
		return m, nil
	}
	if m.openPRNumber() != p.pr || m.filesView == nil {
		p.notice = i18n.T("open the pull request to see the file")
		return m, nil
	}
	for _, l := range m.filesView.visible() {
		if l.path != c.Path {
			continue
		}
		u, cmd := m.openDiffForFileLine(l)
		m = u.(Model)
		if dv := m.diffLayer(); dv != nil {
			m.noteLand = &noteLanding{id: c.ID, tag: m.diffTag}
			if m.topLayer() != dv { // a diff already open BELOW the panel was reused in place: show it
				m = m.removeLayer(dv)
				m = m.pushLayer(dv)
			}
		}
		return m, cmd
	}
	p.notice = i18n.T("%s is not in the pull request's file list", c.Path)
	return m, nil
}

func (p *sendPanel) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if p.typed != "" { // one kept slot: a verdict box's text must not be wiped by an empty panel
			m.keptSendBody = &keptSendBody{pr: p.pr, group: sendGroupPanel, text: p.typed}
		}
		return m.popLayer(), nil
	case tea.KeyUp:
		p.move(-1)
		return m, nil
	case tea.KeyDown:
		p.move(1)
		return m, nil
	case tea.KeyPgUp:
		p.move(-10)
		return m, nil
	case tea.KeyPgDown:
		p.move(10)
		return m, nil
	case tea.KeyHome:
		p.sel, p.notice = 0, ""
		return m, nil
	case tea.KeyEnd:
		p.sel, p.notice = max(0, len(p.rows)-1), ""
		return m, nil
	case tea.KeySpace:
		p.tick()
		return m, nil
	case tea.KeyEnter:
		return p.openRow(m)
	case tea.KeyCtrlS:
		req, ok := p.request()
		if !ok {
			p.notice = i18n.T("tick something to send")
			return m, nil
		}
		if p.gen != m.forgeGen {
			p.notice = i18n.T("the repository changed — open the panel again")
			return m, nil
		}
		if !m.opsIdle() {
			p.notice = i18n.T("another operation is running — send again when it ends")
			return m, nil
		}
		if p.body == bodyTyped {
			m.keptSendBody = &keptSendBody{pr: p.pr, group: sendGroupPanel, text: p.typed}
		}
		return m.forgeSendCmd(req)
	}
	switch msg.String() {
	case "j":
		p.move(1)
	case "k":
		p.move(-1)
	case "a":
		p.tickAll()
	case "b":
		p.cycleBody()
		p.notice = ""
	case "e":
		return p.editBody(m)
	case "c":
		p.code = !p.code
	}
	return m, nil
}

// editBody is e: the body box prefilled with the CURRENT body (spec §5.3) —
// the typed text, or a chosen review's text, read off the UI thread.
func (p *sendPanel) editBody(m Model) (Model, tea.Cmd) {
	if p.body != bodyReview || m.svc == nil {
		return m.pushLayer(&sendPanelBody{panel: p, body: newTextField(p.typed)}), nil
	}
	svc, gen, pr, id := m.svc, m.forgeGen, p.pr, p.bodyFrom
	return m, func() tea.Msg {
		text, err := svc.ReviewBodyText(context.Background(), id)
		return sendPanelBodyMsg{gen: gen, pr: pr, text: text, err: err}
	}
}

// sendPanelBodyMsg is editBody's read of a review's text.
type sendPanelBodyMsg struct {
	gen  int
	pr   int
	text string
	err  error
}

// handleSendPanelBody opens the body box over the panel that asked, prefilled
// with the review's text (an unreadable review: an empty box and a word).
func (m Model) handleSendPanelBody(msg sendPanelBodyMsg) (Model, tea.Cmd) {
	p := layerOf[*sendPanel](m)
	if msg.gen != m.forgeGen || p == nil || p.pr != msg.pr || m.topLayer() != p {
		return m, nil
	}
	text := msg.text
	if msg.err != nil {
		p.notice = i18n.T("send: %s", firstLine(msg.err.Error()))
		text = ""
	}
	return m.pushLayer(&sendPanelBody{panel: p, body: newTextField(text)}), nil
}

// sendPanelBody is e: the typed body's box over the panel; ctrl+s keeps it
// and makes the body "typed", esc drops the edit.
type sendPanelBody struct {
	popupMax
	panel  *sendPanel
	body   textfield
	scroll int
}

func (b *sendPanelBody) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		b.panel.typed = b.body.Value()
		b.panel.body, b.panel.bodyFrom = bodyTyped, ""
		return m.popLayer(), nil
	case tea.KeyEnter:
		b.body.InsertNewline()
		return m, nil
	case tea.KeyUp:
		b.body.Up()
		return m, nil
	case tea.KeyDown:
		b.body.Down()
		return m, nil
	}
	b.body.HandleEditKey(msg)
	return m, nil
}

func (b *sendPanelBody) render(m Model, below string) string {
	w, h := m.overlayDims()
	innerW := popupResolveWidth(w, b.maximized, commitNormalWidth(w))
	contentW := popupTextWidth(innerW)
	var sb strings.Builder
	sb.WriteString(i18n.T("Review body for #%d", b.panel.pr) + "\n\n")
	sb.WriteString(viewFieldWindow("> "+i18n.T("body:")+" ", b.body, true, contentW, 10, &b.scroll) + "\n")
	sb.WriteString("\n" + packHints([]string{i18n.T("[enter] newline"), i18n.T("[ctrl+s] keep"), i18n.T("[ctrl+t] fullscreen"), i18n.T("[esc] cancel")}, contentW))
	return overlayCenter(clipToHeight(below, h), popupBox(innerW, sb.String()), w, h)
}

// render: title + count, the groups in their colour, the rows, the code
// excerpt under the current row, the Body row, the key hints.
func (p *sendPanel) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *sendPanel) box(m Model) string {
	w, h := m.overlayDims()
	innerW := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(innerW)
	title := i18n.T("Send to GitHub — #%d", p.pr)
	count := i18n.T("%d ticked", p.tickedCount())
	head := title + strings.Repeat(" ", max(1, textW-lipgloss.Width(title)-lipgloss.Width(count))) + count
	var rows []winRow
	anchor := 0
	for i, r := range p.rows {
		g := p.cands.Groups[r.group]
		prefix := "  "
		if i == p.sel {
			prefix = "> "
			anchor = len(rows)
		}
		if r.cand < 0 {
			row := winRow{text: prefix + "▌ " + panelGroupTitle(g)}
			if style, ok := groupBarStyle(g.Slot); ok {
				row.style = style.Bold(true)
			} else {
				row.style = lipgloss.NewStyle().Bold(true)
			}
			if i == p.sel {
				row.style = st().selectedRow
			}
			rows = append(rows, row)
			continue
		}
		c := g.Rows[r.cand]
		row := winRow{text: prefix + panelRowText(c, p.ticked[c.ID]), elide: true, elideHead: len([]rune(prefix)) + 10}
		if i == p.sel {
			row.style = st().selectedRow
		} else if c.Skip != "" {
			row.style = st().dim
		}
		rows = append(rows, row)
		if i == p.sel && p.code && c.Skip == "" {
			for _, l := range c.Code {
				rows = append(rows, winRow{text: "          " + sanitizeLine(l), style: st().dim, noWrap: true})
			}
		}
	}
	listH := max(3, min(len(rows), h-12)) // as tall as the list, never past the frame
	lines := renderWindow(rows, winOpts{w: textW, h: listH, anchor: anchor})
	body := i18n.T("Body: %s", p.bodyLabel()) + "   " + i18n.T("[b] change") + "  " + i18n.T("[e] edit")
	hints := packHints([]string{
		i18n.T("[space] tick"), i18n.T("[a] all/none in group"), i18n.T("[enter] open"), i18n.T("[b] body"),
		i18n.T("[e] edit body"), i18n.T("[c] code"), i18n.T("[ctrl+s] send"), i18n.T("[ctrl+t] fullscreen"), i18n.T("[esc] close"),
	}, textW)
	parts := []string{head, ""}
	parts = append(parts, lines...)
	parts = append(parts, "", truncate(body, textW))
	if p.notice != "" {
		parts = append(parts, truncate("▸ "+p.notice, textW))
	}
	parts = append(parts, "", hints)
	return popupBox(innerW, strings.Join(parts, "\n"))
}

// panelGroupTitle: "Review (AI) · <agent> · <title> · <date>" (the title
// only when the review has one), "My notes", "Draft replies".
func panelGroupTitle(g domain.SendCandidateGroup) string {
	switch g.Kind {
	case "mine":
		return i18n.T("My notes")
	case "replies":
		return i18n.T("Draft replies")
	}
	parts := []string{i18n.T("Review (AI)"), g.Agent}
	// The title is the summary's first line, often a markdown heading: its
	// marks are not words.
	if t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(g.Title), "#")); t != "" {
		parts = append(parts, truncate(sanitizeLine(t), 40))
	}
	parts = append(parts, g.Created.Local().Format("2006-01-02 15:04"))
	return strings.Join(parts, " · ")
}

// panelRowText: "[x] high  path:120-134  summary"; a skip row "[ ] ~ … (reason)".
func panelRowText(c domain.SendCandidate, ticked bool) string {
	box := "[ ]"
	if ticked {
		box = "[x]"
	}
	sev := c.Severity
	if c.Skip != "" {
		sev = "~"
	}
	where := c.Path + ":" + strconv.Itoa(c.Range[0])
	if c.Range[1] > c.Range[0] {
		where += "-" + strconv.Itoa(c.Range[1])
	}
	if c.Side == "old" {
		where = c.Path + ":-" + strconv.Itoa(c.Range[0])
	}
	s := box + " " + padCell(sev, 5) + " " + where + "  " + sanitizeLine(c.Summary)
	if c.Skip != "" {
		s += " " + strings.TrimSpace(sendSkipReasonText("", c.Skip))
	}
	return s
}
