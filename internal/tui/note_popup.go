package tui

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// noteFormMode is what the popup will do on ctrl+s.
type noteFormMode int

const (
	noteAdd noteFormMode = iota
	noteEdit
	noteReply
)

// notePopup collects a note's summary and optional rationale — the commit
// popup's title/description pair, with the same keys (tab switches, enter is
// next/newline, ctrl+s saves, esc cancels, ctrl+t maximizes). An empty summary
// on ctrl+s is a cancel (§4.4).
type notePopup struct {
	popupMax
	summary   textfield
	rationale textfield
	link      textfield // reply only: a commit or gg:// link the reply points at
	field     int       // 0 = summary, 1 = rationale, 2 = side (add, when the row has both) or link (reply)
	ratScroll int

	// anchors are the sides a new note may hang off (new first); pick indexes
	// the chosen one. Empty on edit/reply, where the thread fixes the side.
	anchors []noteAnchor
	pick    int

	mode     noteFormMode
	targetID string // noteEdit: the note; noteReply: the parent
	addr     model.FileAddress
	side     model.NoteSide
	first    int // a range note's first line; line is its last (first == line: one line)
	line     int
	hash     string
	ranged   bool // written over the diff's marked lines: a successful save clears the marks
	author   string
	preview  string // add in a preview or pair diff: the scope (PreviewNoteSet.Pair) the note records
	sendPR   int    // Reply & send: the PR the reply goes to once saved (0 = save only)
}

// openNotePopup pushes the form for mode, anchored at the cursor (add) or at
// the note nearest the cursor (edit/reply). Inert when the surface carries no
// address, or when edit/reply have no note to act on.
func (m Model) openNotePopup(mode noteFormMode) (tea.Model, tea.Cmd) {
	if v := m.diffLayer().curNoteView(); v != nil && v.reviewID != "" && mode != noteReply {
		m.statusMsg = m.reviewReadOnlyNotice()
		m.diffNotice = m.statusMsg // the full-screen diff has no status bar
		return m, nil
	}
	// Lines marked in the diff: the note covers them. The address and the
	// scope are read where the MARKS are — frozen ones stay in their file
	// while the cursor walks a stack — so the cursor is lent to their first
	// line for the length of this call (contextLinkText does the same).
	var marked *noteAnchor
	if mode == noteAdd {
		a, row, refusal, on := m.noteAnchorOfMarks()
		if refusal != "" {
			m.statusMsg = refusal
			m.diffNotice = refusal // the full-screen diff has no status bar
			return m, nil
		}
		if row < 0 {
			return m, nil
		}
		if on {
			v := m.diffLayer()
			defer func(cur int) { v.curLine = cur }(v.curLine)
			v.curLine = row
			marked = &a
		}
	}
	if mode == noteReply {
		// Every stored thread and a review's remarks take replies; forge
		// threads are read-only.
		all := m.notesAtCursor()
		ts := replyableNoteTargets(all, m.prOfDiff() > 0)
		if len(ts) == 0 && len(all) > 0 {
			m.statusMsg = i18n.T("forge comments are read-only")
			m.diffNotice = m.statusMsg
			return m, nil
		}
		return m.withNoteTargetIn(ts, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
			return m.openNotePopupFor(mode, t)
		})
	}
	addr, ok := m.diffNoteAddress()
	if !ok {
		return m, nil
	}
	p := &notePopup{mode: mode, addr: addr, author: m.identity.EffectiveName}
	switch mode {
	case noteAdd:
		p.anchors = m.noteAnchorsAtCursor()
		if marked != nil {
			p.anchors, p.ranged = []noteAnchor{*marked}, true
		}
		if len(p.anchors) == 0 {
			// On a preview the cause is knowable and worth saying: the cursor
			// is on a line that exists only on the merge-base side.
			if set := m.previewNoteSet(); set != nil {
				m.statusMsg = oldSideRefusal(set)
			}
			return m, nil
		}
		p.setPick(0)
		p.summary, p.rationale = newTextField(""), newTextField("")
		// A pull request's diff is a preview over forge refs (refs/gg/pr/N),
		// not branch names: its notes record none (the web page agrees).
		if set := m.previewNoteSet(); set != nil && (m.previewOpen == nil || m.previewOpen.prNumber == 0) {
			p.preview = set.Pair()
		}
	case noteEdit:
		return m.withEditableNoteTarget(func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
			return m.openNotePopupFor(mode, t)
		})
	}
	return m.pushLayer(p), nil
}

// noteAnchorOfMarks is the anchor of a note over the diff's marked lines: the
// cursor's side, first to last line (diffLinkSelection's trim rule), with the
// fingerprint of the whole block; row is the view line carrying the first one.
// on == false: no marks, or one marked row with the cursor ON it — that is the
// one-line note at the cursor, side choice included. A non-empty refusal is
// the translated reason these marks take no note; row == -1 says their file
// takes no notes at all (c is inert, never a note at the cursor). Every gate reads the file
// the MARKS are in (the cursor is lent to their first row meanwhile): frozen
// marks stay in their file while the cursor walks a stack.
func (m Model) noteAnchorOfMarks() (a noteAnchor, row int, refusal string, on bool) {
	v := m.diffLayer()
	if v == nil || !v.lsel.on {
		return a, 0, "", false
	}
	lo, hi, _ := v.lsel.bounds(v.curLine)
	lo, hi = max(lo, 0), min(hi, len(v.lines)-1)
	if lo > hi || (lo == hi && lo == v.curLine) {
		return a, 0, "", false
	}
	// Read BEFORE the cursor is lent: loose marks end at the cursor.
	sel, _ := m.diffLinkSelection()
	defer func(cur int) { v.curLine = cur }(v.curLine)
	v.curLine = lo
	if _, ok := m.diffNoteAddress(); !ok {
		return a, -1, "", false // the marks' file takes no notes: c stays inert
	}
	// A preview's old side is the MERGE BASE (noteAnchorsAtCursor's rule).
	if set := m.previewNoteSet(); v.onOld && set != nil {
		return a, 0, oldSideRefusal(set), false
	}
	switch {
	case sel.crossFile:
		return a, 0, i18n.T("▸ a note marks lines of one file"), false
	case sel.refusal != "":
		return a, 0, i18n.T("▸ nothing to note on this side — [esc] unmark"), false
	}
	return noteAnchor{side: sel.side, first: sel.first, line: sel.last, hash: model.NoteContextHash(sel.block)}, sel.row, "", true
}

// oldSideRefusal says why the old side of a preview-scoped diff takes no note,
// in the words of the view on screen: a commit pair (a set with no branch
// names) is a compare, not a preview.
func oldSideRefusal(set *domain.PreviewNoteSet) string {
	if set.IsPair() {
		return i18n.T("notes in a compare anchor on the new side")
	}
	return i18n.T("notes in a preview anchor on the new side")
}

// openNotePopupFor opens the edit/reply form on one targeted note.
func (m Model) openNotePopupFor(mode noteFormMode, t noteTarget) (tea.Model, tea.Cmd) {
	addr, ok := m.diffNoteAddress()
	// A review's remark has no note address here (the review view refuses
	// one); its reply is stored with the review, so none is needed.
	if !ok && !model.IsReviewNoteID(t.rootID) {
		return m, nil
	}
	p := &notePopup{mode: mode, addr: addr, author: m.identity.EffectiveName}
	p.side, p.first, p.line, p.hash = t.side, t.first, t.line, t.hash
	if p.first < 1 || p.first > p.line {
		p.first = p.line
	}
	if mode == noteEdit {
		// Edit acts on the targeted ROW's own note (which may be a reply).
		p.targetID = t.note.ID
		p.summary, p.rationale = newTextField(t.note.Summary), newTextField(t.note.Rationale)
	} else {
		// Reply threads onto the ROOT — NoteReply flattens to it anyway,
		// and inherits the root's address/side/range/fingerprint.
		p.targetID = t.rootID
		p.summary, p.rationale, p.link = newTextField(""), newTextField(""), newTextField("")
	}
	return m.pushLayer(p), nil
}

// setPick chooses anchor i as the note's side/line/fingerprint.
func (p *notePopup) setPick(i int) {
	if len(p.anchors) == 0 {
		return
	}
	p.pick = ((i % len(p.anchors)) + len(p.anchors)) % len(p.anchors)
	a := p.anchors[p.pick]
	p.side, p.first, p.line, p.hash = a.side, a.first, a.line, a.hash
	if p.first == 0 {
		p.first = p.line
	}
}

// hasSideField reports whether the form offers a side choice: only on add,
// and only when the cursor row exists in both versions.
func (p *notePopup) hasSideField() bool { return p.mode == noteAdd && len(p.anchors) > 1 }

// fields is how many tab stops the form has.
func (p *notePopup) fields() int {
	if p.hasSideField() || p.mode == noteReply {
		return 3
	}
	return 2
}

// update handles one key. It swallows every key: esc cancels, ctrl+c quits,
// ctrl+s saves (or cancels on an empty summary).
func (p *notePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		m = m.popLayer()
		if strings.TrimSpace(p.summary.Value()) == "" {
			return m, nil // an empty summary is a cancel
		}
		return m, m.noteSubmitCmd(p)
	case tea.KeyTab:
		p.field = (p.field + 1) % p.fields()
		return m, nil
	case tea.KeyShiftTab:
		p.field = (p.field + p.fields() - 1) % p.fields()
		return m, nil
	case tea.KeyEnter:
		switch p.field {
		case 0:
			p.field = 1
		case 1:
			p.rationale.InsertNewline()
		default:
			p.field = 0
		}
		return m, nil
	case tea.KeyUp:
		if p.field == 1 {
			p.rationale.Up()
		}
		return m, nil
	case tea.KeyDown:
		if p.field == 1 {
			p.rationale.Down()
		}
		return m, nil
	}
	switch {
	case p.field == 0:
		p.summary.HandleEditKey(msg)
	case p.field == 1:
		p.rationale.HandleEditKey(msg)
	case p.mode == noteReply:
		p.link.HandleEditKey(msg)
	default:
		// The side field: ←/→, space, h/l or the side's own letter flip it.
		switch msg.String() {
		case "left", "h", "shift+tab":
			p.setPick(p.pick - 1)
		case "right", "l", " ":
			p.setPick(p.pick + 1)
		case "o":
			p.pickSide(model.NoteSideOld)
		case "n":
			p.pickSide(model.NoteSideNew)
		}
	}
	return m, nil
}

func (p *notePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *notePopup) box(m Model) string {
	w, _ := m.overlayDims()
	innerW := popupResolveWidth(w, p.maximized, commitNormalWidth(w))
	contentW := popupTextWidth(innerW)
	heading := i18n.T("Add note")
	switch p.mode {
	case noteEdit:
		heading = i18n.T("Edit note")
	case noteReply:
		heading = i18n.T("Reply to note")
		if p.sendPR != 0 {
			heading = i18n.T("Reply & send")
		}
	}
	footer := packHints([]string{
		i18n.T("[tab] switch field"),
		i18n.T("[enter] newline/next"),
		i18n.T("[ctrl+t] fullscreen"),
		i18n.T("[ctrl+s] save"),
		i18n.T("[esc] cancel"),
	}, contentW)

	cur := func(i int) string {
		if p.field == i {
			return "> "
		}
		return "  "
	}
	var b strings.Builder
	where := i18n.T("%s line %d", noteSideLabel(p.side), p.line)
	if p.first < p.line {
		where = i18n.T("%s lines %d-%d", noteSideLabel(p.side), p.first, p.line)
	}
	b.WriteString(heading + "  " + where + "\n\n")
	// Field labels follow the commit popup: plain, not translated.
	b.WriteString(viewField(cur(0)+"summary:   ", p.summary, p.field == 0, contentW) + "\n")
	b.WriteString(viewFieldWindow(cur(1)+"rationale: ", p.rationale, p.field == 1, contentW, 8, &p.ratScroll) + "\n")
	if p.mode == noteReply {
		b.WriteString(viewField(cur(2)+"link:      ", p.link, p.field == 2, contentW) + "\n")
		b.WriteString("  " + i18n.T("a commit or gg:// link the reply points at (optional)") + "\n")
	}
	if p.hasSideField() {
		// "side:  ● new R12   ○ old L12" — the picked anchor is filled in.
		var opts []string
		for i, a := range p.anchors {
			dot := "○ "
			if i == p.pick {
				dot = "● "
			}
			opts = append(opts, dot+noteSideLabel(a.side)+" "+noteSideMark(a.side)+strconv.Itoa(a.line))
		}
		line := cur(2) + "side:      " + strings.Join(opts, "   ")
		if p.field == 2 {
			line += "   " + i18n.T("[←/→] pick")
		}
		b.WriteString(truncate(line, contentW) + "\n")
	}
	b.WriteString("\n" + footer)
	return popupBox(innerW, b.String())
}

// noteSubmitCmd performs the popup's write off the UI thread.
func (m Model) noteSubmitCmd(p *notePopup) tea.Cmd {
	svc := m.svc
	if svc == nil {
		return nil
	}
	summary := strings.TrimSpace(p.summary.Value())
	rationale := strings.TrimSpace(p.rationale.Value())
	mode, id, ranged, sendPR, gen := p.mode, p.targetID, p.ranged, p.sendPR, m.forgeGen
	n := p.note(summary, rationale)
	return func() tea.Msg {
		ctx := context.Background()
		var err error
		switch mode {
		case noteEdit:
			err = svc.NoteEdit(ctx, id, summary, rationale)
		case noteReply:
			d, rerr := svc.NoteReply(ctx, id, n)
			if rerr == nil && sendPR != 0 {
				return noteMutatedMsg{clearMarks: ranged, sendPR: sendPR, sendID: d.ID, sendGen: gen}
			}
			err = rerr
		default:
			_, err = svc.NoteAdd(ctx, n)
		}
		return noteMutatedMsg{err: err, clearMarks: ranged}
	}
}

// note is the note the form writes: under line, covering first..line.
func (p *notePopup) note(summary, rationale string) model.Note {
	return model.Note{
		Source: model.NoteSourceUser, Author: p.author, Address: p.addr, Preview: p.preview,
		Side: p.side, Range: [2]int{p.first, p.line}, ContextHash: p.hash,
		Summary: summary, Rationale: rationale, Link: strings.TrimSpace(p.link.Value()),
	}
}

// pickSide selects the anchor on side if the form offers it.
func (p *notePopup) pickSide(side model.NoteSide) {
	for i, a := range p.anchors {
		if a.side == side {
			p.setPick(i)
			return
		}
	}
}

// noteSideLabel is the side's word in the form: "new" or "old".
func noteSideLabel(side model.NoteSide) string {
	if side == model.NoteSideOld {
		return i18n.T("old side")
	}
	return i18n.T("new side")
}

// noteSideMark is the side's letter in a line reference: R for the new side,
// L for the old (the box title uses the same).
func noteSideMark(side model.NoteSide) string {
	if side == model.NoteSideOld {
		return "L"
	}
	return "R"
}
