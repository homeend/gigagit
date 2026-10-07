package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// The review view: a structured AI review (domain.Review.Doc) opens as the
// files view of the reviewed commit — or, for a range review, of its range —
// in a review mode: an "≡ Overview" row first, ◆n on each file the review
// notes, the file's one-line summary under it, and in the diff ONLY the
// review's notes (read-only, built at read time). A prose review opens in the
// viewer with its markdown rendered instead.

// reviewViewState is the files view's review mode. Set after the view opens;
// closeFilesView drops it with the rest of the view.
type reviewViewState struct {
	id     string
	review domain.Review
	counts map[string]int
	other  []domain.ReviewOtherNote
	tip    string
	// states is a working review's files against the worktree (nil for a
	// commit review): the tree marks the ones that changed since.
	states map[string]domain.WorkingFileState
	// back is the commit whose files view opened this review (its @notes/
	// entry): esc returns to that list. Zero = esc closes the view.
	back model.Commit
	// backPreview is the preview (or pair) whose Reviews block opened this
	// review: esc re-opens it. One-shot — leaveReviewView clears it.
	backPreview *previewReturn
	// older: a preview review of a tip its preview has since moved past —
	// the header says so (R2: it still shows exactly what was reviewed).
	older bool
}

// previewReturn is the preview a review view was opened from (its Reviews
// block): esc re-opens it, the cursor on the review's row.
type previewReturn struct {
	id, source, target string             // a merge preview (id "" = a one-off)
	pair               *domain.CommitPair // a commit pair (A, B, Label); nil for a merge preview
	title              string
}

// previewReturnHere is what the view on screen re-opens as when a review is
// opened from its Reviews block: the merge preview (previewOpen) or the
// commit pair (its note scope); nil for any other view.
func (m Model) previewReturnHere() *previewReturn {
	if po := m.previewOpen; po != nil {
		return &previewReturn{id: po.id, source: po.source, target: po.target, title: m.filesTitle}
	}
	if s := m.filesPreviewSet; s != nil && s.IsPair() && s.Only == "" {
		return &previewReturn{pair: &domain.CommitPair{A: s.Base, B: s.Tip, Label: m.filesPairLabel}, title: m.filesTitle}
	}
	return nil
}

// reviewViewMsg is openReview's off-thread read.
type reviewViewMsg struct {
	id, title string
	review    domain.Review
	counts    map[string]int
	other     []domain.ReviewOtherNote
	base, tip string
	isRange   bool
	states    map[string]domain.WorkingFileState
	back      model.Commit
	backPrev  *previewReturn
	older     bool
	gen       int // the loading box it answers (reviewLoadingPopup.gen)
	err       error
}

// openReview opens review id: the review view for a structured review, the
// markdown viewer for a prose one. title names the viewer in the prose case.
// Every entry point — @notes/, a Branches review row, View all notes, the
// AI-tasks tab, a finished review run — comes through here.
func (m Model) openReview(id, title string) (Model, tea.Cmd) {
	return m.openReviewFrom(id, title, model.Commit{})
}

// openReviewFrom is openReview from a commit's files view: esc from the review
// view returns to back's file list.
func (m Model) openReviewFrom(id, title string, back model.Commit) (Model, tea.Cmd) {
	return m.openReviewWith(id, title, back, nil)
}

// openReviewWith is openReviewFrom with a preview to return to (bp, from a
// preview's Reviews block) instead of a commit.
func (m Model) openReviewWith(id, title string, back model.Commit, bp *previewReturn) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil {
		return m, nil
	}
	// The read can take seconds on a slow disk: a box says so and holds the
	// keys until the review shows (esc cancels).
	m.reviewOpenGen++
	gen := m.reviewOpenGen
	m = m.pushLayer(&reviewLoadingPopup{gen: gen})
	return m, func() tea.Msg {
		ctx := context.Background()
		out := reviewViewMsg{id: id, title: title, back: back, backPrev: bp, gen: gen}
		if out.review, out.err = svc.Review(ctx, id); out.err != nil || out.review.Doc == nil {
			return out
		}
		out.base, out.tip, out.isRange = svc.ReviewRevs(ctx, out.review)
		out.older = svc.ReviewOlder(ctx, out.review)
		if out.review.Kind == domain.ReviewOnWorktree {
			out.states = domain.WorkingReviewState(out.review.Worktree, out.review.Files).States
		}
		if out.counts, out.err = svc.ReviewFileCounts(ctx, id); out.err != nil {
			return out
		}
		out.other, out.err = svc.ReviewOtherNotes(ctx, id)
		return out
	}
}

// handleReviewViewMsg opens what openReview read. A popup on the stack is
// parked (handOffToFilesView), so esc from the view returns to it.
func (m Model) handleReviewViewMsg(msg reviewViewMsg) (Model, tea.Cmd) {
	if !m.hasReviewLoading(msg.gen) {
		return m, nil // cancelled (esc on the loading box), or superseded
	}
	m = m.dropReviewLoading()
	if msg.err != nil {
		// Gone (the viewer says "review deleted"), or its commit is gone: the
		// review itself lives in the note, so it still opens, as text.
		m, cmd := m.openReviewNote(msg.id, msg.title)
		if msg.review.ID != "" {
			m.statusMsg = i18n.T("review: %s", msg.err.Error())
		}
		return m, cmd
	}
	if msg.review.Doc == nil {
		m, cmd := m.openReviewNote(msg.id, msg.title)
		m.statusMsg = i18n.T("not in gg review format — shown as text")
		return m, cmd
	}
	st := &reviewViewState{id: msg.id, review: msg.review, counts: msg.counts, other: msg.other, tip: msg.tip, states: msg.states, back: msg.back, backPreview: msg.backPrev, older: msg.older}
	open := func(m Model) (Model, tea.Cmd) {
		var cmd tea.Cmd
		switch {
		case msg.review.Kind == domain.ReviewOnWorktree:
			// A review of uncommitted changes opens as HEAD ↔ the working
			// tree, untracked files included (CompareFiles adds them).
			left, lerr := model.CommitEndpoint(msg.base)
			if lerr != nil {
				m.statusMsg = i18n.T("review: %s", lerr.Error())
				return m, nil
			}
			m.compareTag = ""
			m, cmd = m.openCompareFiles(left, model.WorkTreeEndpoint())
		case msg.isRange:
			left, lerr := model.CommitEndpoint(msg.base)
			right, rerr := model.CommitEndpoint(msg.tip)
			if lerr != nil || rerr != nil {
				m.statusMsg = i18n.T("review: %s", fmt.Sprint(lerr, rerr))
				return m, nil
			}
			m.compareTag = "" // a compare of the same pair re-reads for the review mode
			m, cmd = m.openCompareFiles(left, right)
		default:
			m, cmd = m.openChangedFiles(model.Commit{Hash: msg.tip})
		}
		m.filesReview = st
		// The files view shows only with the Commits focus (the Tags / goto
		// openers do the same); esc restores the panel it was opened from.
		// The tree side: moving the commit list would leave the review.
		m.focus = panelCommits
		m = m.focusTree()
		label := reviewLabel(msg.review)
		if msg.older {
			label = i18n.T("%s · older tip", label)
		}
		m.filesTitle = i18n.T("Review: %s", label)
		return m, cmd
	}
	if m.layers != nil && len(m.layers.entries) > 0 {
		return m.handOffToFilesView(open)
	}
	return open(m)
}

// reviewLabel is the preview a preview review belongs to (the Previews
// panel's "source → target"), else the branch a review was of, else its
// commit's short sha.
func reviewLabel(r domain.Review) string {
	if r.Kind == domain.ReviewOnWorktree {
		return i18n.T("working changes")
	}
	if r.Preview != "" {
		return domain.NoteScopeLabel(r.Preview)
	}
	if r.Branch != "" {
		return r.Branch + " " + shortHash(r.Commit)
	}
	return shortHash(r.Commit)
}

// reviewMetaLine is the review view's line under the title: agent, age and
// how many notes the review holds on how many files.
func reviewMetaLine(st *reviewViewState) string {
	notes, files := 0, 0
	if st.review.Doc != nil {
		notes, files = st.review.Doc.NoteCount()
	}
	parts := []string{}
	if st.review.Agent != "" {
		parts = append(parts, st.review.Agent)
	}
	if !st.review.Created.IsZero() {
		parts = append(parts, ageString(clock.Now(), st.review.Created))
	}
	parts = append(parts, i18n.T("%d notes on %d files", notes, files))
	if _, resolved := st.review.Tally(); resolved > 0 {
		parts = append(parts, i18n.T("%d resolved", resolved))
	}
	return strings.Join(parts, " · ")
}

// reviewTreeLines is the review mode's file list: the Overview row, then the
// commit's files with ◆n on each file the review places notes on and that
// file's summary (dim, not a file) under it.
func reviewTreeLines(st *reviewViewState, files []contentLine) []contentLine {
	summaries := map[string]string{}
	if st.review.Doc != nil {
		for _, f := range st.review.Doc.Files {
			if s := strings.TrimSpace(f.Summary); s != "" {
				summaries[strings.TrimPrefix(f.Path, "./")] = sanitizeLine(s)
			}
		}
	}
	out := []contentLine{{text: "≡ " + i18n.T("Overview"), overview: true}}
	for _, l := range files {
		if l.path == "" {
			out = append(out, l)
			continue
		}
		l.text += noteBadge(st.counts[l.path])
		if st.states != nil {
			switch state, reviewed := st.states[l.path]; {
			case !reviewed:
				l.text += " · " + i18n.T("not reviewed")
			case state != domain.WorkingFileMatches:
				l.text += " · " + i18n.T("changed since the review")
			}
		}
		out = append(out, l)
		if s := summaries[l.path]; s != "" {
			indent := strings.Repeat(" ", len(l.text)-len(strings.TrimLeft(l.text, " "))+3)
			out = append(out, contentLine{text: indent + s, dim: true})
		}
	}
	return out
}

// stepReviewFile moves the review view's cursor to the next (or previous) file
// the review places notes on; at the last one it stays.
func (m Model) stepReviewFile(forward bool) {
	p, st := m.filesView, m.filesReview
	vis := p.visible()
	step := 1
	if !forward {
		step = -1
	}
	for i := p.sel + step; i >= 0 && i < len(vis); i += step {
		if vis[i].path != "" && st.counts[vis[i].path] > 0 {
			p.sel = i
			return
		}
	}
}

// stampReviewNotes marks a diff opened from the review view as the review's:
// its notes are the review's (ReviewNotesFor), never the store's. A range
// review's compare diff gets an address at the tip, where the review's notes
// anchor. Called where stampPreviewNotes is.
func (m Model) stampReviewNotes(dv *diffView, path string) {
	st := m.filesReview
	if dv == nil || st == nil || path == "" {
		return
	}
	dv.reviewID = st.id
	if st.review.Kind == domain.ReviewOnWorktree {
		dv.noteAddr = model.FileAddress{State: model.StateUnstaged, Worktree: st.review.Worktree, Path: path}
		return
	}
	if dv.noteAddr.Path == "" {
		dv.noteAddr = model.FileAddress{State: model.StateCommitted, Commit: st.tip, Path: path}
	}
}

// reviewReadOnlyNotice is what a note gesture in the review view says: the
// review's notes are part of the review, not the store.
func (m Model) reviewReadOnlyNotice() string {
	if st := m.filesReview; st != nil && st.review.Kind == domain.ReviewOnWorktree {
		return i18n.T("▸ review notes are read-only") // --notes does not apply to a working review
	}
	return i18n.T("▸ review notes are read-only — gg review --notes keeps them")
}

// reviewOverviewPopup is the review's overview: its markdown rendered, its
// meta, and the notes the tree cannot place ("Other notes"). It is prose to
// read, so it has no row cursor; o lists the other notes to open one, y copies
// the overview's markdown.
type reviewOverviewPopup struct {
	*contentPopup
	st *reviewViewState
}

// openReviewOverview pushes the overview of the open review view.
func (m Model) openReviewOverview() (Model, tea.Cmd) {
	st := m.filesReview
	if st == nil || st.review.Doc == nil {
		return m, nil
	}
	cp := newContentPopup(i18n.T("Review: %s", reviewLabel(st.review)), reviewOverviewLines(st))
	cp.mode = modeWrap // prose
	cp.noCursor = true
	cp.prose = true
	cp.keys = i18n.T("[y] copy")
	if n := len(st.other); n > 0 {
		cp.keys += "  " + i18n.T("[o] other notes (%d)", n)
	}
	return m.pushLayer(&reviewOverviewPopup{contentPopup: cp, st: st}), nil
}

// reviewOverviewLines lays out the overview: the markdown, the document's
// meta, then the other notes as "path:line — summary" rows.
func reviewOverviewLines(st *reviewViewState) []contentLine {
	doc := st.review.Doc
	out := prMarkdownLines(doc.Overview, "")
	if len(doc.Meta) > 0 {
		out = append(out, contentLine{text: ""}, contentLine{text: reviewMetaText(doc.Meta)})
	}
	if len(st.other) > 0 {
		out = append(out, contentLine{text: ""}, contentLine{text: i18n.T("Other notes"), heading: true})
		for _, l := range reviewOtherNoteLines(st) {
			l.text = "  " + l.text
			out = append(out, l)
		}
	}
	return out
}

// reviewOtherNoteLines is one "path:line — summary" row per note the tree
// cannot place, carrying its path.
func reviewOtherNoteLines(st *reviewViewState) []contentLine {
	out := make([]contentLine, 0, len(st.other))
	heading := false
	for _, o := range st.other {
		if o.Outdated {
			// A thread whose remark the re-saved review no longer has: never
			// a path:line, under one heading.
			if !heading {
				out = append(out, contentLine{text: i18n.T("Outdated (the remark is gone from the re-saved review)"), heading: true})
				heading = true
			}
			out = append(out, contentLine{text: sanitizeLine(o.Summary) + threadSuffix(len(o.Replies), o.Resolution != nil)})
			continue
		}
		line := fmt.Sprint(o.Range[0])
		if o.Side == model.NoteSideOld {
			line = "-" + line
		}
		text := o.Path + ":" + line + " — " + sanitizeLine(o.Summary)
		if o.Changed {
			text += " (" + i18n.T("changed since the review") + ")"
		}
		text += threadSuffix(len(o.Replies), o.Resolution != nil)
		out = append(out, contentLine{text: text, path: o.Path})
	}
	return out
}

// threadSuffix is a remark row's thread state: " — 2 replies · resolved".
func threadSuffix(replies int, resolved bool) string {
	var parts []string
	switch {
	case replies == 1:
		parts = append(parts, i18n.T("1 reply"))
	case replies > 1:
		parts = append(parts, i18n.T("%d replies", replies))
	}
	if resolved {
		parts = append(parts, i18n.T("resolved"))
	}
	if len(parts) == 0 {
		return ""
	}
	return " — " + strings.Join(parts, " · ")
}

// reviewMetaText is meta as "key: value · key: value".
func reviewMetaText(meta []notebatch.MetaKV) string {
	parts := make([]string, len(meta))
	for i, kv := range meta {
		parts[i] = sanitizeLine(kv.Key + ": " + kv.Value)
	}
	return strings.Join(parts, " · ")
}

func (p *reviewOverviewPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if !p.typing { // while the / filter is capturing, every key is query text
		switch msg.String() {
		case "y":
			return m, m.copyToClipboardCmd(i18n.T("copied the review overview"), p.st.review.Doc.Overview)
		case "o":
			if len(p.st.other) == 0 {
				return m, nil
			}
			cp := newContentPopup(i18n.T("Other notes"), reviewOtherNoteLines(p.st))
			cp.prose = true
			cp.keys = i18n.T("[enter] open the file at the reviewed commit")
			return m.pushLayer(&reviewOtherNotesPopup{contentPopup: cp, tip: p.st.tip}), nil
		case "enter":
			return m, nil // prose: nothing to pick
		}
	}
	return p.contentPopup.update(m, msg)
}

// reviewOtherNotesPopup lists the review's notes the tree cannot place (a
// path outside the reviewed change); enter opens that file at the commit.
type reviewOtherNotesPopup struct {
	*contentPopup
	tip string
}

func (p *reviewOtherNotesPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if !p.typing && msg.String() == "enter" {
		vis := p.visible()
		if p.sel >= 0 && p.sel < len(vis) && vis[p.sel].path != "" {
			if p.tip == "" { // a working review: no commit to open the file at
				m.statusMsg = i18n.T("the file changed since the review")
				return m, nil
			}
			return m.openFileAtCommit(p.tip, vis[p.sel].path)
		}
		return m, nil
	}
	return p.contentPopup.update(m, msg)
}

// openFileAtCommit opens path as it is at commit rev in the viewer.
func (m Model) openFileAtCommit(rev, path string) (Model, tea.Cmd) {
	src := fileSource{kind: srcCommit, rev: rev}
	d := m.openFiles.find(m.currentWorktree, docKey(src, path))
	if d == nil {
		d = m.newOpenFile(src, path)
	} else {
		m = m.detachDoc(d)
	}
	m = m.pushLayer(&fileViewer{d})
	m = m.registerDoc(d)
	return m, m.loadDoc(d)
}

// reviewLoadingPopup is the box shown while a review is read: it holds the
// keys (nothing else may start meanwhile) until the review shows; esc cancels.
type reviewLoadingPopup struct{ gen int }

func (p *reviewLoadingPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.String() == "esc" {
		return m.popLayer(), nil // the read lands later and opens nothing
	}
	return m, nil
}

func (p *reviewLoadingPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	body := i18n.T("Opening the review…") + "\n\n" + i18n.T("[esc] cancel")
	return overlayCenter(clipToHeight(below, h), popupBox(popupInnerWidth(w), body)+"\n", w, h)
}

// hasReviewLoading reports the loading box of open gen still up.
func (m Model) hasReviewLoading(gen int) bool {
	p := layerOf[*reviewLoadingPopup](m)
	return p != nil && p.gen == gen
}

// dropReviewLoading takes the loading box off the stack.
func (m Model) dropReviewLoading() Model {
	if m.layers == nil {
		return m
	}
	for i, l := range m.layers.entries {
		if _, ok := l.(*reviewLoadingPopup); ok {
			entries := append([]layer{}, m.layers.entries[:i]...)
			m.layers = &layerStack{entries: append(entries, m.layers.entries[i+1:]...)}
			return m
		}
	}
	return m
}
