package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// The Previews list shows a preview's (or commit pair's) AI reviews as
// sub-rows under it (spec R3), like the Branches tab's review sub-rows:
// pvEntry rows, so sorting, the filter and backingIndex treat them as the
// Branches tab treats a branch's.

type pvEntry struct {
	pv     int    // index into m.previews
	review string // a review sub-row: its note id ("" = the saved row itself)
}

// sub reports a review sub-row, not a saved row.
func (e pvEntry) sub() bool { return e.review != "" }

// previewEntries is the Previews list in backing order: each row followed by
// its reviews (readPreviews classified them: current, then older tips).
func (m Model) previewEntries() []pvEntry {
	out := make([]pvEntry, 0, len(m.previews))
	for i, r := range m.previews {
		out = append(out, pvEntry{pv: i})
		for _, h := range r.reviews {
			out = append(out, pvEntry{pv: i, review: h.ID})
		}
	}
	return out
}

// previewReviewRowBody is a sub-row without its indent: the Branches
// sub-row's text, plus " · older tip" for a review of a tip the preview has
// moved past (R2: it still opens exactly what was reviewed).
func previewReviewRowBody(h domain.ReviewHead) string {
	body := branchReviewRowBody(h)
	if h.Older {
		body += " · " + i18n.T("older tip")
	}
	return body
}

func (m Model) previewReviewHead(pv int, id string) (domain.ReviewHead, bool) {
	if pv < 0 || pv >= len(m.previews) {
		return domain.ReviewHead{}, false
	}
	for _, h := range m.previews[pv].reviews {
		if h.ID == id {
			return h, true
		}
	}
	return domain.ReviewHead{}, false
}

func (m Model) selectedPreviewEntry() (pvEntry, bool) {
	idx := m.displayIndices(panelPreviews)
	sel := m.sel[panelPreviews]
	if sel < 0 || sel >= len(idx) {
		return pvEntry{}, false
	}
	ents := m.previewEntries()
	if idx[sel] >= len(ents) {
		return pvEntry{}, false
	}
	return ents[idx[sel]], true
}

// selectedPreviewReview resolves a review sub-row under the Previews cursor.
func (m Model) selectedPreviewReview() (domain.ReviewHead, bool) {
	if m.focus != panelPreviews {
		return domain.ReviewHead{}, false
	}
	e, ok := m.selectedPreviewEntry()
	if !ok || !e.sub() {
		return domain.ReviewHead{}, false
	}
	return m.previewReviewHead(e.pv, e.review)
}

// previewDisplayIndex is the display row of saved row bi — a steer landing's
// target, never one of its sub-rows.
func (m Model) previewDisplayIndex(bi int) (int, bool) {
	ents := m.previewEntries()
	for di, u := range m.displayIndices(panelPreviews) {
		if u < len(ents) && !ents[u].sub() && ents[u].pv == bi {
			return di, true
		}
	}
	return 0, false
}

// previewReviewRowMenu is a review sub-row's . menu (R6): the review row
// menu every other surface offers — open, copy its link — with Delete review
// (deleteReviewRow) after it.
func (m Model) previewReviewRowMenu() []actionRow {
	h, ok := m.selectedPreviewReview()
	if !ok || m.inContentWindow() || m.svc == nil {
		return nil
	}
	svc, id := m.svc, h.ID
	return []actionRow{
		{id: "open-review", label: i18n.T("Open review"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openReview(h.ID, h.Summary)
		}},
		m.asyncCopyLinkRow("copy-gg-link", i18n.T("Copy gg link"), func(ctx context.Context) (string, error) {
			return svc.ReviewLink(ctx, id)
		}),
	}
}

// previewReviewLines puts a scope's reviews on top of an opened preview's
// file list under a "Reviews" heading, replacing a block already there (a
// refresh re-applies it). Each row is a noteID row: enter opens the review
// view, esc comes back here.
func previewReviewLines(heads []domain.ReviewHead, lines []contentLine) []contentLine {
	rest := lines
	if len(rest) > 1 && rest[0].heading && rest[1].noteID != "" {
		i := 1
		for i < len(rest) && rest[i].noteID != "" {
			i++
		}
		rest = rest[i:]
	}
	if len(heads) == 0 {
		return rest
	}
	out := make([]contentLine, 0, len(heads)+1+len(rest))
	out = append(out, contentLine{text: i18n.T("Reviews"), heading: true})
	for _, h := range heads {
		// No path: a review is not a file, so every file action passes it by.
		out = append(out, contentLine{text: "  " + previewReviewRowBody(h), noteID: h.ID})
	}
	return append(out, rest...)
}

// setPreviewReviews stores the open scope's heads and re-lays the list on
// screen, keeping the cursor on the row it was on; a pending landing
// (filesLandNote: back from a review) puts it on that review's row. A list
// still loading takes the heads when it lands (the compareFilesMsg arm).
func (m Model) setPreviewReviews(heads []domain.ReviewHead) Model {
	m.filesPreviewReviews = heads
	p := m.filesView
	if p == nil || m.filesReview != nil || !m.inCompareMode() ||
		(len(p.lines) == 1 && isLoadingPlaceholder(p.lines[0].text)) {
		return m
	}
	var keep contentLine
	if vis := p.visible(); p.sel >= 0 && p.sel < len(vis) {
		keep = vis[p.sel]
	}
	p.lines = previewReviewLines(heads, p.lines)
	return m.landPreviewCursor(keep)
}

// landPreviewCursor puts the tree cursor on filesLandNote's row when it is
// listed (consuming it), else back on keep's row (the same file or review).
func (m Model) landPreviewCursor(keep contentLine) Model {
	p := m.filesView
	if p == nil {
		return m
	}
	vis := p.visible()
	if id := m.filesLandNote; id != "" {
		for i, l := range vis {
			if l.noteID == id {
				m.filesLandNote = ""
				p.sel = i
				return m
			}
		}
	}
	for i, l := range vis {
		if (keep.path != "" && l.path == keep.path) || (keep.noteID != "" && l.noteID == keep.noteID) {
			p.sel = i
			return m
		}
	}
	if p.sel >= len(vis) {
		p.sel = max(len(vis)-1, 0)
	}
	return m
}
