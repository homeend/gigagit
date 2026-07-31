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
func (m Model) startEntryCompare(left, right entrySide) (Model, tea.Cmd) {
	// Same commit on both sides is a non-compare — except two DIFFERENT shelf
	// entries of the same commit, whose frozen sets may legitimately differ.
	distinctShelves := left.shelfID != "" && right.shelfID != "" && left.shelfID != right.shelfID
	if left.sha == right.sha && !distinctShelves {
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
