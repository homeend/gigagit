package tui

import (
	"context"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// previewRowKind says which saved change-set a Previews row is. The ZERO kind
// is the merge preview, deliberately and against the package's Invalid-first
// habit: every producer and fixture that predates pairs builds
// previewRow{rec: …}, and a row with a filled rec and no kind IS a merge
// preview. The value never leaves this package.
type previewRowKind int

const (
	rowMerge previewRowKind = iota
	rowPair                 // a saved commit pair: two frozen shas (domain.CommitPair)
	// rowCompare is a saved COMPARISON: two gg:// links (a two-link
	// savedcompare entry), re-run through domain.CompareLinks. Not a "pair":
	// that word is rowPair's, one link holding @<a>..<b>. Every site that
	// branches on merge() must handle this kind BEFORE assuming rowPair — a
	// comparison row's pair is ZERO, exactly as a pair row's rec is.
	rowCompare
)

// previewRow is one saved change-set with its summary: a merge preview (rec,
// sum, notes) or a commit pair (pair, psum). A row whose summary read failed
// keeps err (rendered as the state text).
//
// rec is read ONLY through merge(), and only in this file (a gate test holds
// that): a pair row's rec is ZERO, and handing its empty Source/Target to a
// merge-preview path ends in a confusing git error rather than a refusal.
type previewRow struct {
	kind    previewRowKind
	rec     model.MergePreview
	pair    domain.CommitPair
	psum    domain.PairSummary
	cmp     domain.SavedCompare // rowCompare: the two link texts, id, label
	cmpDesc [2]string           // rowCompare: each link's one-line description
	sym     *domain.Symmetric   // rowCompare: non-nil when it is a symmetric merge preview (domain.SymmetricOf)
	sum     domain.PreviewSummary
	notes   int            // root notes gathered along the branch, hidden ones included
	byPath  map[string]int // the same counts per path; feeds the open preview's file list
	// reviews are the row's AI reviews (spec R2: one of the current tip, or
	// of an older tip whose commits still exist), newest first.
	reviews []domain.ReviewHead
	err     error
}

// merge is the row as a merge preview; false for a pair.
func (r previewRow) merge() (model.MergePreview, bool) { return r.rec, r.kind == rowMerge }

// id, label and created are the kind-agnostic identity of a row: what the
// list keys, names and sorts by.
func (r previewRow) id() string {
	switch r.kind {
	case rowPair:
		return r.pair.ID
	case rowCompare:
		return r.cmp.ID
	}
	return r.rec.ID
}

func (r previewRow) label() string {
	switch r.kind {
	case rowPair:
		return r.pair.Label
	case rowCompare:
		return r.cmp.Label
	}
	return r.rec.Label
}

func (r previewRow) created() time.Time {
	switch r.kind {
	case rowPair:
		return r.pair.Created
	case rowCompare:
		return r.cmp.Created
	}
	return r.rec.Created
}

// subject is the middle column: what the row is a diff OF.
func (r previewRow) subject() string {
	switch r.kind {
	case rowPair:
		return shortHash(r.pair.A) + ".." + shortHash(r.pair.B)
	case rowCompare:
		if r.sym != nil {
			// ASCII badge: a new glyph must be EAW-neutral, and ↔ reads as a
			// left arrow in a monospace cell.
			return "sym  " + i18n.T("%s vs %s", r.sym.A, r.sym.B) + "  " + i18n.T("base: %s", r.sym.Base)
		}
		return r.cmpDesc[0] + " ↔ " + r.cmpDesc[1]
	}
	return r.rec.Source + " → " + r.rec.Target
}

// compare is the row as a saved comparison; false for the other two kinds.
func (r previewRow) compare() (domain.SavedCompare, bool) { return r.cmp, r.kind == rowCompare }

// previewsPayload is srcPreviews' dataAvailableMsg value.
type previewsPayload struct{ rows []previewRow }

// readPreviews lists the records, summarises each and counts its notes. An
// unchanged pair costs two rev-parse calls for the summary (name → hash is how
// movement is detected) and no diff work; only a moved pair runs the three
// summary calls. A previewable pair then costs roughly two more, because
// PreviewNotes resolves the pair again to gather the commit range — parked for
// the final wave rather than threaded through here.
func readPreviews(ctx context.Context, svc *domain.Service) (previewsPayload, error) {
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		return previewsPayload{}, err
	}
	rows := make([]previewRow, 0, len(ps))
	for _, p := range ps {
		sum, err := svc.PreviewSummary(ctx, p.Source, p.Target)
		row := previewRow{rec: p, sum: sum, err: err}
		// Ruling 6: a pair that is not previewable has no note scope at all,
		// so it gets no badge and costs no store read.
		if err == nil && sum.State == domain.PreviewOK {
			if set, serr := svc.PreviewNotes(ctx, p.Source, p.Target); serr == nil {
				if byPath, total, cerr := svc.PreviewNoteCounts(ctx, set); cerr == nil {
					row.notes, row.byPath = total, byPath
				}
				row.reviews, _ = svc.PreviewReviews(ctx, set) // best-effort, like the counts
			}
		} else if err == nil {
			// Merged, a side missing, no base: no scope can be built, but a
			// review whose commits still exist stays listed as an older tip
			// (spec §8).
			row.reviews, _ = svc.PreviewReviewsByScope(ctx, p.Target+"..."+p.Source)
		}
		rows = append(rows, row)
	}
	// Saved commit pairs follow the merge previews. A frozen pair's summary
	// is cached by its two shas in domain, so a refresh costs two rev-parse
	// existence checks per pair and no diff.
	pairs, err := svc.PairList(ctx)
	if err != nil {
		return previewsPayload{}, err
	}
	for _, p := range pairs {
		psum, err := svc.PairSummary(ctx, p.A, p.B)
		row := previewRow{kind: rowPair, pair: p, psum: psum, err: err}
		// The same badge a merge row carries: root notes along a..b.
		if err == nil && psum.State == domain.PairOK {
			if set, serr := svc.PairNotes(ctx, p.A, p.B); serr == nil {
				if byPath, total, cerr := svc.PreviewNoteCounts(ctx, set); cerr == nil {
					row.notes, row.byPath = total, byPath
				}
				row.reviews, _ = svc.PreviewReviews(ctx, set)
			}
		}
		rows = append(rows, row)
	}
	// Saved COMPARISONS last: the two-link entries. The SET-shaped entries are
	// the rows above (a merge preview, a commit pair), reached through their
	// own façades — so exactly the non-set ones are taken here, or every
	// preview would be listed twice. A row costs two descriptions and no
	// comparison: it is evaluated when it is opened, and a link that no longer
	// resolves keeps its row (it can still be renamed, copied and removed).
	saved, err := svc.SavedCompareList(ctx)
	if err != nil {
		return previewsPayload{}, err
	}
	for _, c := range saved {
		if c.IsSet() {
			continue
		}
		row := previewRow{kind: rowCompare, cmp: c,
			cmpDesc: [2]string{describeLinkText(ctx, svc, c.Left), describeLinkText(ctx, svc, c.Right)}}
		if sym, ok := domain.SymmetricOf(c); ok {
			row.sym = &sym
		}
		rows = append(rows, row)
	}
	return previewsPayload{rows: rows}, nil
}

// previewList is the panelList behind the Previews tab, entry-based like
// branchList: each saved row is followed by one sub-row per AI review of it
// (previewEntries). A sub-row's Name and Date are its row's, so the stable
// sort keeps it under its parent; backingIndex refuses it, so every preview
// action self-gates on a sub-row.
type previewList struct {
	rows []previewRow
	ents []pvEntry
	text []string // one per entry
}

func (l previewList) Len() int          { return len(l.ents) }
func (l previewList) Row(i int) string  { return l.text[i] }
func (l previewList) Name(i int) string { return l.rows[l.ents[i].pv].label() }
func (l previewList) Date(i int) int64  { return l.rows[l.ents[i].pv].created().Unix() }

// Key is the record id for a saved row (stable across renames and refreshes;
// selection, marks and steer landings key on it) and id\x00review for a
// sub-row.
func (l previewList) Key(i int) string {
	k := l.rows[l.ents[i].pv].id()
	if r := l.ents[i].review; r != "" {
		k += "\x00" + r
	}
	return k
}

// Haystack is the filter-match text: a row and its sub-rows match as one
// unit (branchList's rule), the row WITHOUT its ◆N note badge (the
// statusList contract — typing a digit never matches a pair because of how
// many notes it carries).
func (l previewList) Haystack(i int) string {
	p := i
	for p > 0 && l.ents[p].sub() {
		p--
	}
	var sb strings.Builder
	sb.WriteString(strings.TrimSuffix(l.text[p], noteBadge(l.rows[l.ents[p].pv].notes)))
	for q := p + 1; q < len(l.ents) && l.ents[q].sub(); q++ {
		sb.WriteByte(' ')
		sb.WriteString(l.text[q])
	}
	return sb.String()
}

// previewStateText is the right-hand cell: counts when ok, else the state.
func previewStateText(r previewRow) string {
	if r.err != nil {
		return i18n.T("error: %s", r.err.Error())
	}
	if r.kind == rowCompare {
		return "" // nothing is evaluated until the row is opened
	}
	if r.kind == rowPair {
		switch r.psum.State {
		case domain.PairMissingA:
			return i18n.T("missing commit: %s", shortHash(r.pair.A))
		case domain.PairMissingB:
			return i18n.T("missing commit: %s", shortHash(r.pair.B))
		}
		if r.psum.Files == 1 {
			return i18n.T("1 file")
		}
		return i18n.T("%d files", r.psum.Files)
	}
	switch r.sum.State {
	case domain.PreviewMerged:
		return i18n.T("merged")
	case domain.PreviewMissingSource:
		return i18n.T("missing: %s", r.rec.Source)
	case domain.PreviewMissingTarget:
		return i18n.T("missing: %s", r.rec.Target)
	case domain.PreviewNoBase:
		return i18n.T("no common base")
	}
	if r.sum.Files == 1 {
		return i18n.T("1 file  ↑%d", r.sum.Ahead)
	}
	return i18n.T("%d files  ↑%d", r.sum.Files, r.sum.Ahead)
}

// previewRows renders the Previews list: one row per entry (previewEntries).
func (m Model) previewRows() []string { return m.previewRowsFor(m.previewEntries()) }

// previewRowsFor renders one row per entry: a saved row as "<label>
// <source → target>  <state>" with the label column padded to the widest
// label (display width, CJK-safe), a review sub-row indented under it.
func (m Model) previewRowsFor(ents []pvEntry) []string {
	labels := make([]string, len(m.previews))
	for i, r := range m.previews {
		labels[i] = r.label()
	}
	w := maxLabelWidth(8, labels...)
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.sub() {
			if h, ok := m.previewReviewHead(e.pv, e.review); ok {
				out = append(out, "  "+previewReviewRowBody(h))
			} else {
				out = append(out, "  └ ?")
			}
			continue
		}
		r := m.previews[e.pv]
		// The SAME ◆N badge every other note-bearing row wears (it brings its
		// own leading gap), never a glyph of this panel's own.
		out = append(out, padCell(r.label(), w)+"  "+r.subject()+"  "+previewStateText(r)+noteBadge(r.notes))
	}
	return out
}

// selectedPreview is the focused row when the Previews tab has one.
func (m Model) selectedPreview() (previewRow, bool) {
	i, ok := m.backingIndex(panelPreviews)
	if !ok || i >= len(m.previews) {
		return previewRow{}, false
	}
	return m.previews[i], true
}
