package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// comparePopupKind selects which picker opens in compare mode for a pending
// compare (the "first pick" awaits a second from this list).
type comparePopupKind int

const (
	compareBookmark comparePopupKind = iota // open the bookmark popup
	compareShelf                            // open the shelf popup
)

// pendingCompare carries the focused file (frozen at menu time) across the
// async list load, so the popup it produces opens in compare mode. It lives on
// the Model, never on the not-yet-built popup (mirrors the reword-prefill fix).
// target decides which picker (bookmark vs shelf) consumes it.
type pendingCompare struct {
	ref    model.FileRef
	entry  *entrySide // non-nil = the first pick is a commit entry (ref is then unused)
	label  string
	target comparePopupKind
}

// openCompareFocusedVsBookmark diffs the focused file (ref, left/old) against a
// picked bookmark (bm, right/new) in the full-screen diff view.
func (m Model) openCompareFocusedVsBookmark(ref model.FileRef, label string, bm model.Bookmark) (Model, tea.Cmd) {
	width, _ := m.overlayDims()
	v := &diffView{
		title:   ref.Path + " ↔ " + bm.Path,
		context: label + " → " + bookmarkDisplay(bm),
		compare: true,
		loading: true,
		partial: m.diffPartial,
		long:    m.diffLong,
		width:   width,
	}
	return m.openPickerDiff(v, "cmpbm:"+ref.Path+":"+bm.ID, m.loadCompareFocusedVsBookmarkCmd(ref, label, bm))
}

// openCompareFocusedVsShelf diffs the focused file (ref, left/old) against a
// picked shelf entry (e, right/new). Both sides resolve via ResolveBytes.
func (m Model) openCompareFocusedVsShelf(ref model.FileRef, label string, e model.ShelfEntry) (Model, tea.Cmd) {
	right := model.FileRef{Source: model.SourceShelf, Locator: e.ID, Path: e.Origin.Path}
	width, _ := m.overlayDims()
	shelfLabel := i18n.T("shelf #%s", shortShelf(e))
	v := &diffView{
		title:   ref.Path + " ↔ " + e.Origin.Path,
		context: label + " → " + shelfLabel,
		compare: true,
		loading: true,
		partial: m.diffPartial,
		long:    m.diffLong,
		width:   width,
	}
	tag := "cmpsh:" + ref.Path + ":" + e.ID
	return m.openPickerDiff(v, tag, m.loadCompareTwoRefsCmd(ref, right, ref.Path+" ↔ "+e.Origin.Path, label+" → "+shelfLabel, tag))
}

// loadCompareTwoRefsCmd resolves both sides via ResolveBytes and runs the Differ.
func (m Model) loadCompareTwoRefsCmd(left, right model.FileRef, title, subtitle, tag string) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	v := &diffView{title: title, context: subtitle, partial: m.diffPartial, long: m.diffLong}
	v.width, _ = m.overlayDims()
	return func() tea.Msg {
		oldSrc := func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, left) }
		newSrc := func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, right) }
		out, err := differ.Diff(context.Background(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
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
		compare: true,
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
