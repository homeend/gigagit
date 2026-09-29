package tui

import (
	"context"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// The "View all notes" popup (command palette): every note this checkout can
// see, as a tree — group (working tree / commits / other) → state, commit or
// shelf entry → directory → file → note — with each note row laid out in
// fixed columns (status, who, where, when, note). Enter on a note opens its
// diff over the popup and lands on it; esc on that diff comes back here.

// allNotesRows is the unmaximized visible-row budget; ctrl+t trades it for
// the terminal-derived cap.
const allNotesRows = 16

// Column geometry of a note row. Every note row starts its columns at the
// same indent, whatever its depth, so STATUS is one column down the list.
const (
	anNoteIndent = 10
	anStatusW    = 9
	anWhoW       = 10
	anWhereW     = 10
	anWhenW      = 8
)

type anRowKind int

const (
	anGroup  anRowKind = iota // Working tree / Commits / Other (foldable)
	anSub                     // Unstaged/Staged/Untracked, a commit, a shelf entry (foldable)
	anDir                     // a directory heading
	anFile                    // a file
	anNote                    // one thread
	anReview                  // one AI review stored on a commit (under @notes/)
)

// anTarget is where a file or note row opens.
type anTarget struct {
	state   model.FileState
	commit  string // StateCommitted
	subject string // the commit heading, for the diff's context line
	path    string
	oldPath string
	change  string // the commit's own change letter; "" = unchanged there
	missing bool   // the commit or shelf entry is gone
}

type anRow struct {
	kind   anRowKind
	depth  int
	text   string // headings and files: the label; notes: unused
	key    string // fold key (groups and subs)
	span   int    // index one past the row's last descendant
	note   *domain.ResolvedNote
	review *domain.Review // anReview rows
	status string         // a note's display status
	target anTarget
	filter string // lowercased text a query matches (notes only)
}

type allNotesPopup struct {
	popupMax
	loading bool
	err     string
	rows    []anRow
	folded  map[string]bool
	query   string
	sel     int
	notice  string // why the last enter could not open anything
	// Set by box() for render's bottom bar: the selected row's uncut text
	// when it was cut ("" = shown in full).
	tipFull string
}

// allNotesMsg carries the overview, tagged with the loadGen it was asked
// under so a repo switch drops it.
type allNotesMsg struct {
	ov  domain.NotesOverview
	err error
	gen int
}

// openAllNotes pushes the popup in its loading state and reads the overview
// off the UI thread.
func (m Model) openAllNotes() (Model, tea.Cmd) {
	if layerOf[*allNotesPopup](m) != nil {
		return m, nil
	}
	m = m.pushLayer(&allNotesPopup{loading: true, folded: map[string]bool{}})
	svc, gen := m.svc, m.loadGen
	return m, func() tea.Msg {
		ov, err := svc.NotesOverview(context.Background())
		return allNotesMsg{ov: ov, err: err, gen: gen}
	}
}

// onAllNotes lands the overview in the open popup.
func (m Model) onAllNotes(msg allNotesMsg) (Model, tea.Cmd) {
	p := layerOf[*allNotesPopup](m)
	if p == nil || msg.gen != m.loadGen {
		return m, nil
	}
	p.loading = false
	if msg.err != nil {
		p.err = msg.err.Error()
		return m, nil
	}
	p.rows = buildAllNotesRows(msg.ov, filepath.Base(m.currentWorktree))
	// A re-read after a delete can be shorter than the cursor: keep it on a row.
	if n := len(p.visible()); p.sel >= n {
		p.sel = max(n-1, 0)
	}
	return m, nil
}

// buildAllNotesRows flattens the overview into the tree, depth-first, and
// stamps each row's span so folding and filtering can skip a subtree.
func buildAllNotesRows(ov domain.NotesOverview, worktree string) []anRow {
	var rows []anRow
	files := func(depth int, fs []domain.NoteFileNotes, t anTarget) {
		lastDir := ""
		for _, f := range fs {
			dir := path.Dir(f.Addr.Path)
			if dir == "." {
				dir = ""
			}
			if dir != "" && dir != lastDir {
				rows = append(rows, anRow{kind: anDir, depth: depth, text: dir + "/"})
			}
			lastDir = dir
			ft := t
			ft.state, ft.path = f.Addr.State, f.Addr.Path
			ft.change, ft.oldPath = f.Status, f.OldPath
			rows = append(rows, anRow{kind: anFile, depth: depth + 1, text: path.Base(f.Addr.Path), target: ft})
			for i := range f.Notes {
				r := &f.Notes[i]
				status := string(r.Status)
				if t.missing {
					status = "missing"
				}
				rows = append(rows, anRow{kind: anNote, depth: depth + 2, note: r, status: status, target: ft,
					filter: strings.ToLower(r.Note.Summary + "\x00" + r.Note.Author + "\x00" + f.Addr.Path)})
			}
		}
	}
	group := func(key, text string) {
		rows = append(rows, anRow{kind: anGroup, depth: 0, text: text, key: key})
	}
	sub := func(key, text string) {
		rows = append(rows, anRow{kind: anSub, depth: 1, text: text, key: key})
	}

	if len(ov.Unstaged)+len(ov.Staged)+len(ov.Untracked) > 0 {
		label := i18n.T("Working tree")
		if worktree != "" && worktree != "." {
			label += "  (" + worktree + ")"
		}
		group("wt", label)
		for _, s := range []struct {
			key, label string
			fs         []domain.NoteFileNotes
		}{
			{"wt:unstaged", i18n.T("Unstaged"), ov.Unstaged},
			{"wt:staged", i18n.T("Staged"), ov.Staged},
			{"wt:untracked", i18n.T("Untracked"), ov.Untracked},
		} {
			if len(s.fs) == 0 {
				continue
			}
			sub(s.key, s.label)
			files(2, s.fs, anTarget{})
		}
	}
	if len(ov.Commits) > 0 {
		group("commits", i18n.T("Commits"))
		for _, c := range ov.Commits {
			label := shortHash(c.Hash) + "  "
			if c.Missing {
				label += i18n.T("(missing — rewritten or deleted)")
			} else {
				label += sanitizeLine(c.Subject)
				if c.UnixTime > 0 {
					label += "  · " + time.Unix(c.UnixTime, 0).Local().Format(model.CommitDateLayout)
				}
			}
			sub("c:"+c.Hash, label)
			if len(c.Reviews) > 0 {
				rows = append(rows, anRow{kind: anDir, depth: 2, text: i18n.T("Reviews")})
				for i := range c.Reviews {
					r := &c.Reviews[i]
					rows = append(rows, anRow{kind: anReview, depth: 3, review: r,
						filter: strings.ToLower(r.Summary + "\x00" + r.Agent + "\x00" + r.Branch)})
				}
			}
			files(2, c.Files, anTarget{commit: c.Hash, subject: shortHash(c.Hash) + " " + sanitizeLine(c.Subject), missing: c.Missing})
		}
	}
	if len(ov.Shelves) > 0 {
		group("other", i18n.T("Other"))
		for _, s := range ov.Shelves {
			name := s.Label
			if name == "" {
				name = s.ID
			}
			sub("s:"+s.ID, i18n.T("shelf")+"  "+sanitizeLine(name))
			files(2, s.Files, anTarget{missing: s.Missing})
		}
	}

	for i := range rows {
		j := i + 1
		for j < len(rows) && rows[j].depth > rows[i].depth {
			j++
		}
		rows[i].span = j
	}
	return rows
}

// anStatusLabel is a note status's translated column text.
func anStatusLabel(status string) string {
	switch status {
	case string(model.NoteActive):
		return i18n.T("active")
	case string(model.NoteStale):
		return i18n.T("stale")
	case string(model.NoteOrphaned):
		return i18n.T("orphaned")
	case "missing":
		return i18n.T("missing")
	}
	return status
}

// visible is the rows on screen: with a query, the matching notes and their
// ancestors (folds ignored — a match must be seen); without one, every row
// not under a folded heading.
func (p *allNotesPopup) visible() []anRow {
	var out []anRow
	if p.query != "" {
		q := strings.ToLower(p.query)
		match := make([]bool, len(p.rows))
		for i, r := range p.rows {
			match[i] = (r.kind == anNote || r.kind == anReview) && strings.Contains(r.filter, q)
		}
		for i, r := range p.rows {
			keep := match[i]
			if r.kind != anNote && r.kind != anReview {
				for j := i + 1; j < r.span && !keep; j++ {
					keep = match[j]
				}
			}
			if keep {
				out = append(out, r)
			}
		}
		return out
	}
	for i := 0; i < len(p.rows); i++ {
		r := p.rows[i]
		out = append(out, r)
		if r.key != "" && p.folded[r.key] {
			i = r.span - 1
		}
	}
	return out
}

func (p *allNotesPopup) move(d int) {
	n := len(p.visible())
	if n == 0 {
		p.sel = 0
		return
	}
	p.sel = min(max(p.sel+d, 0), n-1)
}

func (p *allNotesPopup) setQuery(q string) {
	p.query, p.sel, p.notice = q, 0, ""
}

// setFold folds or unfolds the heading under the cursor. On a row that is not
// a heading, ← goes to its parent heading instead.
func (p *allNotesPopup) setFold(fold bool) {
	vis := p.visible()
	if p.sel < 0 || p.sel >= len(vis) || p.query != "" {
		return
	}
	r := vis[p.sel]
	if r.key != "" {
		p.folded[r.key] = fold
		p.move(0)
		return
	}
	if !fold {
		return
	}
	for i := p.sel - 1; i >= 0; i-- {
		if vis[i].key != "" && vis[i].depth < r.depth {
			p.sel = i
			return
		}
	}
}

func (p *allNotesPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if filterMotion(msg, p.move, popupFilterPage) {
		p.notice = ""
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlD: // delete the review or thread under the cursor (asks first)
		u, cmd := m.allNotesDelete(p)
		return u.(Model), cmd
	case tea.KeyHome:
		p.sel = 0
	case tea.KeyEnd:
		p.move(len(p.visible()))
	case tea.KeyLeft:
		p.setFold(true)
	case tea.KeyRight:
		p.setFold(false)
	case tea.KeyEnter:
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) {
			return m, nil
		}
		r := vis[p.sel]
		switch r.kind {
		case anGroup, anSub:
			if p.query == "" {
				p.folded[r.key] = !p.folded[r.key]
			}
			return m, nil
		case anFile:
			return m.openAllNotesTarget(p, r.target, "")
		case anNote:
			return m.openAllNotesTarget(p, r.target, r.note.Note.ID)
		case anReview:
			// The review lives in the note: it opens (as text) even when the
			// commit is gone.
			return m.openReview(r.review.ID, r.review.Summary)
		}
	case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete:
		if rs := []rune(p.query); len(rs) > 0 {
			p.setQuery(string(rs[:len(rs)-1]))
		}
	case tea.KeySpace:
		p.setQuery(p.query + " ")
	case tea.KeyRunes:
		p.setQuery(p.query + string(msg.Runes))
	}
	return m, nil
}

// openAllNotesTarget opens t's diff over the popup and, when id is set, parks
// a landing on that note for when the diff's notes arrive.
func (m Model) openAllNotesTarget(p *allNotesPopup, t anTarget, id string) (Model, tea.Cmd) {
	p.notice = ""
	if m.width > 0 && m.width < 60 {
		p.notice = i18n.T("terminal too narrow for the diff view")
		return m, nil
	}
	var cmd tea.Cmd
	switch t.state {
	case model.StateCommitted:
		if t.missing {
			p.notice = i18n.T("That commit no longer exists.")
			return m, nil
		}
		line := contentLine{path: t.path, oldPath: t.oldPath, status: t.change}
		if line.status == "" {
			line.status = "M"
		}
		lm := m
		lm.filesContext = t.subject
		cmd = lm.loadCommitDiffCmd(t.commit, line)
		m = m.pushLayer(&diffView{title: t.path, context: "@ " + t.subject, rev: t.commit, loading: true,
			partial: m.diffPartial, long: m.diffLong,
			noteAddr: model.FileAddress{State: model.StateCommitted, Commit: t.commit, Path: t.path}})
		m.diffTag = "commit:" + t.commit + ":" + t.path
	case model.StateUnstaged, model.StateUntracked, model.StateStaged:
		staged := t.state == model.StateStaged
		pnl := panelFiles
		if staged {
			pnl = panelStaged
		}
		bi, ok := m.steerStatusRow(pnl, t.path)
		if !ok {
			switch t.state {
			case model.StateStaged:
				p.notice = i18n.T("The file has no staged changes now.")
			case model.StateUntracked:
				p.notice = i18n.T("The file is no longer untracked.")
			default:
				p.notice = i18n.T("The file has no unstaged changes now.")
			}
			return m, nil
		}
		f := m.status.Files[bi]
		cmd = m.loadStatusDiffCmd(f, staged)
		m = m.pushLayer(&diffView{title: f.Path, context: statusDiffContext(staged), loading: true,
			partial: m.diffPartial, long: m.diffLong, noteAddr: m.statusNoteAddress(f, staged)})
		m.diffTag = statusDiffTag(f.Path, staged)
	default:
		p.notice = i18n.T("Shelf notes open from gg note list.")
		return m, nil
	}
	m.diffNotice = ""
	m.diffNav = diffNavNone // no source list to step through
	if id != "" {
		m.noteLand = &noteLanding{id: id, tag: m.diffTag}
	}
	return m, cmd
}

func (p *allNotesPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	out := overlayCenter(clipToHeight(below, h), p.box(m), w, h)
	if p.tipFull != "" {
		// The selected row was cut: its full text takes the bottom bar, which
		// the popup never covers, instead of an overlay on the row itself.
		text := " " + p.tipFull
		// Plain text in the terminal's own colours: a highlighted bar flashing
		// up on every cut row was painful in low light (user report).
		out = overlayAt(out, padRight(truncate(text, w), w), 0, h-1, w, h)
	}
	return out
}

// anNoteParts is a note row's fixed columns (head), its summary and its
// reply count (tail); anNoteColumns lays them out to a width, the tooltip
// joins them uncut.
func anNoteParts(r anRow, now time.Time) (head, summary, tail string) {
	n := r.note.Note
	who := n.Author
	if who == "" {
		if n.Source == model.NoteSourceAgent {
			who = i18n.T("agent")
		} else {
			who = i18n.T("you")
		}
	}
	where := string(n.Side) + ":" + strconv.Itoa(r.note.Range[0])
	if r.note.Range[1] != r.note.Range[0] {
		where += "-" + strconv.Itoa(r.note.Range[1])
	}
	if isFileLevelNote(*r.note) {
		where = i18n.T("file")
	}
	when := ""
	if !n.Created.IsZero() {
		when = coarseAgo(now.Sub(n.Created))
	}
	head = padRight(truncate(anStatusLabel(r.status), anStatusW-1), anStatusW) +
		padRight(truncate(sanitizeLine(who), anWhoW-1), anWhoW) +
		padRight(truncate(where, anWhereW-1), anWhereW) +
		padRight(truncate(when, anWhenW-1), anWhenW)
	if len(r.note.Replies) > 0 {
		tail = "  ↩" + strconv.Itoa(len(r.note.Replies))
	}
	return head, sanitizeLine(n.Summary), tail
}

// anReviewParts is a review row's columns, laid out like a note's: STATUS
// "review", WHO the agent, WHERE its branch relation, WHEN its age.
func anReviewParts(r anRow, now time.Time) (head, summary string) {
	status, who, where, when := anReviewCells(r, now)
	head = padRight(truncate(status, anStatusW-1), anStatusW) +
		padRight(truncate(who, anWhoW-1), anWhoW) +
		padRight(truncate(where, anWhereW-1), anWhereW) +
		padRight(truncate(when, anWhenW-1), anWhenW)
	return head, sanitizeLine(r.review.Summary)
}

// anReviewFull is a review row's cells uncut: the bottom bar's text.
func anReviewFull(r anRow, now time.Time) string {
	status, who, where, when := anReviewCells(r, now)
	return strings.Join([]string{status, who, where, when, sanitizeLine(r.review.Summary)}, " · ")
}

// anReviewCells is a review row's column values.
func anReviewCells(r anRow, now time.Time) (status, who, where, when string) {
	v := r.review
	where = i18n.T("commit")
	switch v.Kind {
	case domain.ReviewOnBranch:
		where = i18n.T("branch %s", v.Branch)
	case domain.ReviewWasTip:
		where = i18n.T("was tip %s", v.Branch)
	}
	who = v.Agent
	if who == "" {
		who = i18n.T("agent")
	}
	if !v.Created.IsZero() {
		when = coarseAgo(now.Sub(v.Created))
	}
	return i18n.T("review"), sanitizeLine(who), where, when
}

// anNoteColumns renders a note row's columns (after its indent) to w: the
// summary is the elastic part, so the reply count survives a narrow popup.
func anNoteColumns(r anRow, w int, now time.Time) string {
	head, summary, tail := anNoteParts(r, now)
	budget := w - lipgloss.Width(head) - lipgloss.Width(tail)
	if budget < 1 {
		return truncate(head+tail, w)
	}
	return head + truncate(summary, budget) + tail
}

// anRowText renders one row (without the cursor prefix) to w columns.
func (p *allNotesPopup) anRowText(r anRow, w int, now time.Time) string {
	switch r.kind {
	case anNote:
		return strings.Repeat(" ", anNoteIndent) + anNoteColumns(r, w-anNoteIndent, now)
	case anReview:
		head, summary := anReviewParts(r, now)
		budget := w - anNoteIndent - lipgloss.Width(head)
		if budget < 1 {
			return strings.Repeat(" ", anNoteIndent) + truncate(head, w-anNoteIndent)
		}
		return strings.Repeat(" ", anNoteIndent) + head + truncate(summary, budget)
	case anGroup, anSub:
		mark := "▾ "
		if p.folded[r.key] && p.query == "" {
			mark = "▸ "
		}
		return truncate(strings.Repeat("  ", r.depth)+mark+r.text, w)
	}
	// Directories and files are paths: cut in the middle, keeping the name.
	indent := strings.Repeat("  ", r.depth+1)
	return indent + elidePath(r.text, w-len(indent))
}

// anBarText is what the bottom bar shows for a cut row: the row's own text,
// without the tree's indent or its fold marker.
func anBarText(r anRow, now time.Time) string {
	if r.kind == anNote {
		head, summary, tail := anNoteParts(r, now)
		return head + summary + tail
	}
	if r.kind == anReview {
		return anReviewFull(r, now)
	}
	return r.text
}

// anRowFull is a row's uncut text (without the cursor prefix): what the
// bottom bar shows when anRowText had to cut it.
func (p *allNotesPopup) anRowFull(r anRow, now time.Time) string {
	switch r.kind {
	case anNote:
		head, summary, tail := anNoteParts(r, now)
		return strings.Repeat(" ", anNoteIndent) + head + summary + tail
	case anReview:
		return strings.Repeat(" ", anNoteIndent) + anReviewFull(r, now)
	case anGroup, anSub:
		mark := "▾ "
		if p.folded[r.key] && p.query == "" {
			mark = "▸ "
		}
		return strings.Repeat("  ", r.depth) + mark + r.text
	}
	return strings.Repeat("  ", r.depth+1) + r.text
}

func (p *allNotesPopup) box(m Model) string {
	w, h := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)
	now := time.Now()
	p.tipFull = ""

	keys := []string{
		i18n.T("[↑/↓] move"),
		i18n.T("[enter] open / fold"),
		i18n.T("[←/→] fold"),
	}
	if vis := p.visible(); p.sel >= 0 && p.sel < len(vis) && (vis[p.sel].kind == anNote || vis[p.sel].kind == anReview) {
		keys = append(keys, i18n.T("[ctrl+d] delete"))
	}
	keys = append(keys, i18n.T("type to filter"), i18n.T("[ctrl+t] fullscreen"), i18n.T("[esc] close"))
	hints := wrapParts(keys, textW, "  ")

	header := i18n.T("All notes")
	if n := p.count(); n > 0 {
		header += "  " + strconv.Itoa(n)
	}
	if p.query != "" {
		header += "  " + p.query + "█"
	}
	parts := []string{truncate(header, textW), ""}

	var body []string
	switch {
	case p.loading:
		body = []string{padRight("  "+i18n.T("(loading…)"), textW)}
	case p.err != "":
		body = []string{padRight("  "+truncate(p.err, textW-2), textW)}
	case len(p.rows) == 0:
		body = []string{padRight("  "+i18n.T("No notes in this repository."), textW)}
	default:
		cols := strings.Repeat(" ", 2+anNoteIndent) +
			padRight(i18n.T("STATUS"), anStatusW) + padRight(i18n.T("WHO"), anWhoW) +
			padRight(i18n.T("WHERE"), anWhereW) + padRight(i18n.T("WHEN"), anWhenW) + i18n.T("NOTE")
		parts = append(parts, st().dim.Render(padRight(truncate(cols, textW), textW)))
		vis := p.visible()
		if len(vis) == 0 {
			body = []string{padRight("  "+i18n.T("(no matching notes)"), textW)}
			break
		}
		rows := make([]winRow, len(vis))
		for i, r := range vis {
			prefix := "  "
			var style lipgloss.Style
			if i == p.sel {
				prefix, style = "> ", st().selectedRow
			} else if r.kind == anGroup || r.kind == anSub || r.kind == anDir {
				style = lipgloss.NewStyle().Bold(true)
			}
			rows[i] = winRow{text: prefix + p.anRowText(r, textW-2, now), style: style}
			// The selected row is reverse video: a foreground there would
			// paint a per-glyph background, so it stays plain.
			if i != p.sel && r.kind == anNote {
				rows[i].decorate = anNoteDecorator(r)
			}
			if i != p.sel && r.kind == anReview {
				rows[i].decorate = anColumnsDecorator(st().noteFrameAgent, false)
			}
		}
		// The chrome is the header pair, the column row, the blank + hints
		// and the box border; the list gets what is left of the terminal.
		room := h - (len(parts) + 1 + 1 + len(hints) + 2)
		cap := min(popupResolveRowCap(p.maximized, h, allNotesRows), max(room, 3))
		winH := min(len(rows), cap)
		body = renderWindow(rows, winOpts{w: textW, anchor: p.sel, h: winH})
		if p.sel >= 0 && p.sel < len(vis) {
			if r := vis[p.sel]; rowTruncated(p.anRowFull(r, now), textW-2) {
				p.tipFull = anBarText(r, now)
			}
		}
	}
	parts = append(parts, body...)
	if p.notice != "" {
		parts = append(parts, "", truncate("▸ "+p.notice, textW))
	}
	parts = append(parts, "")
	parts = append(parts, hints...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// anNoteDecorator paints a note row's WHO cell in its author's frame colour
// (agent violet, user blue) and dims the STATUS cell of a note that is not
// active. It paints only when the cell text sits where the layout put it, so
// a panned or clipped row is left alone.
func anNoteDecorator(r anRow) rowDecorator {
	s := st()
	who := s.noteFrameUser
	if r.note.Note.Source == model.NoteSourceAgent {
		who = s.noteFrameAgent
	}
	return anColumnsDecorator(who, r.status != string(model.NoteActive))
}

// anColumnsDecorator paints the WHO cell in who and, when dimStatus, dims the
// STATUS cell — shared by note and review rows, which lay out the same columns.
func anColumnsDecorator(who lipgloss.Style, dimStatus bool) rowDecorator {
	s := st()
	const statusAt = 2 + anNoteIndent // the cursor prefix, then the indent
	return func(visible string, hscroll, visualLine int) string {
		if hscroll != 0 || visualLine != 0 {
			return visible
		}
		rs := []rune(visible)
		paint := func(at, w int, style lipgloss.Style) {
			if at+w > len(rs) {
				return
			}
			cell := strings.TrimRight(string(rs[at:at+w]), " ")
			if cell == "" {
				return
			}
			n := len([]rune(cell))
			painted := []rune(style.Render(cell))
			out := append(append(append([]rune{}, rs[:at]...), painted...), rs[at+n:]...)
			rs = out
		}
		// Right to left, so the earlier cell's offsets stay valid.
		paint(statusAt+anStatusW, anWhoW, who)
		if dimStatus {
			paint(statusAt, anStatusW, s.dim)
		}
		return string(rs)
	}
}

// count is the number of threads the popup lists.
func (p *allNotesPopup) count() int {
	n := 0
	for _, r := range p.rows {
		if r.kind == anNote || r.kind == anReview {
			n++
		}
	}
	return n
}
