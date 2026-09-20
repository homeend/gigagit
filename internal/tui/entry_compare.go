package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// entrySide is one side of a commit-entry comparison: the full sha the entry
// stores, the shelf entry id when the side is a shelved commit ("" for a
// bookmark), and the human label used in notices.
type entrySide struct {
	sha     string
	shelfID string
	label   string
}

func bookmarkEntrySide(b model.Bookmark) entrySide {
	return entrySide{sha: b.Commit, label: bookmarkDisplay(b)}
}

func shelfEntrySide(e model.ShelfEntry) entrySide {
	return entrySide{sha: e.Origin.Commit, shelfID: e.ID, label: i18n.T("shelf #%s", shortShelf(e))}
}

// entryCompareMsg carries both resolved endpoints (or the failure) back to
// the UI thread; gen-guarded by Model.entryCompareGen.
type entryCompareMsg struct {
	gen         int
	left, right model.Endpoint
	err         error
}

// startEntryCompare resolves both sides off the UI thread (hybrid: the live
// sha while it exists, a shelved side's frozen tar after a gc) and then opens
// the whole-tree compare files view. First pick = left/older. Gen-guarded so
// a resolve landing after switcher close or reRoot is dropped.
// sameEntryCommit reports a non-compare: the same commit on both sides —
// except two DIFFERENT shelf entries of one commit, whose frozen sets may
// legitimately differ.
func sameEntryCommit(left, right entrySide) bool {
	distinctShelves := left.shelfID != "" && right.shelfID != "" && left.shelfID != right.shelfID
	return left.sha == right.sha && !distinctShelves
}

func (m Model) startEntryCompare(left, right entrySide) (Model, tea.Cmd) {
	if sameEntryCommit(left, right) {
		m.statusMsg = i18n.T("select a different commit to compare against")
		return m, nil
	}
	m.entryCompareGen++
	gen := m.entryCompareGen
	svc := m.svc
	return m, func() tea.Msg {
		ctx := context.Background()
		l, err := svc.ResolveCommitEntryEndpoint(ctx, left.sha, left.shelfID)
		if err != nil {
			return entryCompareMsg{gen: gen, err: err}
		}
		r, err := svc.ResolveCommitEntryEndpoint(ctx, right.sha, right.shelfID)
		if err != nil {
			return entryCompareMsg{gen: gen, err: err}
		}
		return entryCompareMsg{gen: gen, left: l, right: r}
	}
}

// bookmarkLink and shelfEntryLink are the ONE producer of an entry's gg://
// link: its own address plus the ?<kind>=<id> hint. The switchers' L key and
// the cross compare both come here.
func (m Model) bookmarkLink(b model.Bookmark) (string, bool) {
	return m.hintedLinkFor(b.Address(), model.LinkHint{Kind: "bookmark", ID: b.ID})
}

// shelfEntryLink carries the ORIGIN address plus ?shelf=<id>. The hint is
// load-bearing here, not decoration: for a file shelved from the working tree
// it is the only content source, and for a shelved commit whose sha is gone it
// is what lets the comparison read the frozen copy (spec §3.3's exceptions).
func (m Model) shelfEntryLink(e model.ShelfEntry) (string, bool) {
	return m.hintedLinkFor(e.Origin, model.LinkHint{Kind: "shelf", ID: e.ID})
}

// startCrossCompare compares the two picks of the bookmark↔shelf flow as
// LINKS, through domain.CompareLinks. That is what makes commit↔file legal
// (a whole tree against one member) where the endpoint compare had to refuse.
// commits are the picks that are commit entries: each is checked first, so a
// dead bookmark still gets gg's own "no longer available" rather than the
// door's unknown revision (a shelved commit passes — its frozen copy stands in).
func (m Model) startCrossCompare(left, right string, commits ...entrySide) (Model, tea.Cmd) {
	svc := m.svc
	return m.startLinkCompareAfter(left, right, func(ctx context.Context) error {
		for _, c := range commits {
			if _, err := svc.ResolveCommitEntryEndpoint(ctx, c.sha, c.shelfID); err != nil {
				return err
			}
		}
		return nil
	})
}
