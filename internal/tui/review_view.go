package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	// back is the commit whose files view opened this review (its @notes/
	// entry): esc returns to that list. Zero = esc closes the view.
	back model.Commit
}

// reviewViewMsg is openReview's off-thread read.
type reviewViewMsg struct {
	id, title string
	review    domain.Review
	counts    map[string]int
	other     []domain.ReviewOtherNote
	base, tip string
	isRange   bool
	back      model.Commit
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
	svc := m.svc
	if svc == nil {
		return m, nil
	}
	return m, func() tea.Msg {
		ctx := context.Background()
		out := reviewViewMsg{id: id, title: title, back: back}
		if out.review, out.err = svc.Review(ctx, id); out.err != nil || out.review.Doc == nil {
			return out
		}
		out.base, out.tip, out.isRange = svc.ReviewRevs(ctx, out.review)
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
	st := &reviewViewState{id: msg.id, review: msg.review, counts: msg.counts, other: msg.other, tip: msg.tip, back: msg.back}
	open := func(m Model) (Model, tea.Cmd) {
		var cmd tea.Cmd
		if msg.isRange {
			left, lerr := model.CommitEndpoint(msg.base)
			right, rerr := model.CommitEndpoint(msg.tip)
			if lerr != nil || rerr != nil {
				m.statusMsg = i18n.T("review: %s", fmt.Sprint(lerr, rerr))
				return m, nil
			}
			m.compareTag = "" // a compare of the same pair re-reads for the review mode
			m, cmd = m.openCompareFiles(left, right)
		} else {
			m, cmd = m.openChangedFiles(model.Commit{Hash: msg.tip})
		}
		m.filesReview = st
		m.filesTreeFocused = true // moving the commit list would leave the review
		m.filesTitle = i18n.T("Review: %s", reviewLabel(msg.review))
		return m, cmd
	}
	if m.layers != nil && len(m.layers.entries) > 0 {
		return m.handOffToFilesView(open)
	}
	return open(m)
}

// reviewLabel is the branch a review was of, else its commit's short sha.
func reviewLabel(r domain.Review) string {
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
		parts = append(parts, ageString(time.Now(), st.review.Created))
	}
	parts = append(parts, i18n.T("%d notes on %d files", notes, files))
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
		if n := st.counts[l.path]; n > 0 {
			l.text += fmt.Sprintf("  ◆%d", n)
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
	if dv.noteAddr.Path == "" {
		dv.noteAddr = model.FileAddress{State: model.StateCommitted, Commit: st.tip, Path: path}
	}
}

// reviewReadOnlyNotice is what a note gesture in the review view says: the
// review's notes are part of the review, not the store.
func reviewReadOnlyNotice() string {
	return i18n.T("▸ review notes are read-only — gg review --notes keeps them")
}

// reviewOverviewPopup is the review's overview: its markdown rendered, its
// meta, and the notes the tree cannot place ("Other notes") — enter on one
// opens that file at the commit; y copies the overview's markdown.
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
	cp.footer = i18n.T("[y] copy  [enter] open an other note's file")
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
		for _, o := range st.other {
			line := fmt.Sprint(o.Range[0])
			if o.Side == model.NoteSideOld {
				line = "-" + line
			}
			out = append(out, contentLine{text: "  " + o.Path + ":" + line + " — " + sanitizeLine(o.Summary), path: o.Path})
		}
	}
	return out
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
		case "enter":
			vis := p.visible()
			if p.sel >= 0 && p.sel < len(vis) && vis[p.sel].path != "" {
				return m.openFileAtCommit(p.st.tip, vis[p.sel].path)
			}
			return m, nil
		}
	}
	return p.contentPopup.update(m, msg)
}

// openFileAtCommit opens path as it is at commit rev in the viewer.
func (m Model) openFileAtCommit(rev, path string) (Model, tea.Cmd) {
	src := fileSource{kind: srcCommit, rev: rev}
	d := m.openFiles.find(m.currentWorktree, docKey(src, path))
	if d == nil {
		d = newOpenFile(src, path)
	} else {
		m = m.detachDoc(d)
	}
	m = m.pushLayer(&fileViewer{d})
	m = m.registerDoc(d)
	return m, m.loadDoc(d)
}
