package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/model"
)

// pendingCompare carries the focused file (frozen at menu time) across the
// async bookmark load, so the popup it produces opens in compare mode. It lives
// on the Model, never on the not-yet-built popup (mirrors the reword-prefill fix).
type pendingCompare struct {
	ref   model.FileRef
	label string
}

// openCompareFocusedVsBookmark diffs the focused file (ref, left/old) against a
// picked bookmark (bm, right/new) in the full-screen diff view.
func (m Model) openCompareFocusedVsBookmark(ref model.FileRef, label string, bm model.Bookmark) (Model, tea.Cmd) {
	width, _ := m.overlayDims()
	v := &diffView{
		title:   ref.Path + " ↔ " + bm.Path,
		context: label + " → " + bookmarkDisplay(bm),
		loading: true,
		partial: m.diffPartial,
		long:    m.diffLong,
		width:   width,
	}
	return m.openBookmarkDiff(v, "cmpbm:"+ref.Path+":"+bm.ID, m.loadCompareFocusedVsBookmarkCmd(ref, label, bm))
}

// loadCompareFocusedVsBookmarkCmd resolves the focused side by address
// (ResolveBytes) and the bookmark side via BookmarkBytes, then runs the Differ.
func (m Model) loadCompareFocusedVsBookmarkCmd(ref model.FileRef, label string, bm model.Bookmark) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	tag := "cmpbm:" + ref.Path + ":" + bm.ID
	v := &diffView{
		title:   ref.Path + " ↔ " + bm.Path,
		context: label + " → " + bookmarkDisplay(bm),
		partial: m.diffPartial,
		long:    m.diffLong,
	}
	v.width, _ = m.overlayDims()
	return func() tea.Msg {
		oldSrc := func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, ref) }
		newSrc := func(ctx context.Context) ([]byte, error) { return svc.BookmarkBytes(ctx, bm) }
		out, err := differ.Diff(context.Background(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}
