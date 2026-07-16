package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// shelfRemoveCmd removes an entry then reloads + reopens the popup.
func (m Model) shelfRemoveCmd(entryID string) tea.Cmd {
	svc := m.svc
	reload := m.loadShelfCmd(true) // reopen the popup after the remove
	return func() tea.Msg {
		if err := svc.ShelfRemove(context.Background(), entryID); err != nil {
			return shelfLoadedMsg{err: err}
		}
		return reload()
	}
}

// --- restore destination popup -------------------------------------------

// shelfRestorePopup collects the (mandatory) restore destination.
type shelfRestorePopup struct {
	popupMax
	entryID string
	origin  string    // origin path; prefills dest and backs the ctrl+r re-fill
	dest    textfield // destination (prefilled with origin, freely editable)
}

// update handles one key while the restore popup is open (the overlay contract).
func (p *shelfRestorePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer() // back to the shelf switcher beneath
	case tea.KeyEnter:
		dest := strings.TrimSpace(p.dest.Value())
		if dest == "" {
			return m, nil // a destination is mandatory
		}
		entry := p.entryID
		m = m.popLayer() // back to the switcher; it stays visible during the write
		blob, err := m.svc.ShelfBlob(context.Background(), entry)
		if err != nil {
			m.statusMsg = i18n.T("shelf restore: %s", err.Error())
			return m, nil
		}
		// engine.WriteFile owns the Overwrite/Cancel fork via the modal decider.
		return m.startOp(engine.WriteFile{Path: dest, Data: blob})
	case tea.KeyCtrlR:
		p.dest = newTextField(p.origin) // re-fill with the origin path, cursor at end
	default:
		p.dest.HandleEditKey(msg)
	}
	return m, nil
}

// render draws the restore-destination dialog composited over `below`.
func (p *shelfRestorePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString(i18n.T("Restore shelved file") + "\n\n")
	b.WriteString(i18n.T("from: %s  (shelved copy)", p.origin) + "\n")
	b.WriteString(viewField(i18n.T("dest: "), p.dest, true, popupContentWidth(w)) + "\n\n")
	b.WriteString(i18n.T("[type] path  [enter] restore  [ctrl+r] original path  [esc] cancel"))
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// --- compare (entry vs working tree) -------------------------------------

// openShelfCompareEntry diffs entry e (old) against the current working-tree
// file at its origin path (new). Routed through openPickerDiff so it works when
// invoked from the popup over a stacked surface.
func (m Model) openShelfCompareEntry(e model.ShelfEntry) (Model, tea.Cmd) {
	width, _ := m.overlayDims()
	v := &diffView{title: e.Origin.Path, context: "shelf #" + shortShelf(e) + " → working tree", rev: "", loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	return m.openPickerDiff(v, "shelf:"+e.ID, m.loadShelfCompareCmd(e))
}

// openShelfRestore pushes the restore popup for entry e over the shelf
// switcher (which stays beneath; esc/success returns to it). The dest is
// prefilled with the origin path — restoring in place is the common case, and
// engine.WriteFile's Overwrite/Cancel fork guards the clobber; ctrl+r re-fills
// it after an edit. (Bookmark paste instead prefills a _RESTORED variant: a
// paste is a copy-beside, a restore is a put-back.)
func (m Model) openShelfRestore(e model.ShelfEntry) (Model, tea.Cmd) {
	return m.pushLayer(&shelfRestorePopup{entryID: e.ID, origin: e.Origin.Path, dest: newTextField(e.Origin.Path)}), nil
}

func shortShelf(e model.ShelfEntry) string {
	if len(e.SHA) > 8 {
		return e.SHA[:8]
	}
	return e.SHA
}

func (m Model) shelfEntryByID(id string) (model.ShelfEntry, bool) {
	for _, e := range m.shelfEntries {
		if e.ID == id {
			return e, true
		}
	}
	return model.ShelfEntry{}, false
}

// openShelfCompareTwoEntries diffs entries a (old) and b (new).
func (m Model) openShelfCompareTwoEntries(a, b model.ShelfEntry) (Model, tea.Cmd) {
	title := a.Origin.Path
	if a.Origin.Path != b.Origin.Path {
		title = a.Origin.Path + " ↔ " + b.Origin.Path
	}
	ctx := "shelf #" + shortShelf(a) + " → shelf #" + shortShelf(b)
	width, _ := m.overlayDims()
	v := &diffView{title: title, context: ctx, rev: "", loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	return m.openPickerDiff(v, "shelf2:"+a.ID+":"+b.ID, m.loadShelfCompareTwoCmd(a, b, title, ctx))
}

func (m Model) loadShelfCompareTwoCmd(a, b model.ShelfEntry, title, ctx string) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	tag := "shelf2:" + a.ID + ":" + b.ID
	v := &diffView{title: title, context: ctx, rev: "", partial: m.diffPartial, long: m.diffLong}
	v.width, _ = m.overlayDims()
	aID, bID := a.ID, b.ID
	return func() tea.Msg {
		oldSrc := func(ctx context.Context) ([]byte, error) { return svc.ShelfBlob(ctx, aID) }
		newSrc := func(ctx context.Context) ([]byte, error) { return svc.ShelfBlob(ctx, bID) }
		out, err := differ.Diff(context.Background(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}

// loadShelfCompareCmd resolves both sides off the UI thread: the shelf blob
// (old) and the working-tree file at the entry's origin path (new, nil when
// absent), then feeds the existing Differ.
func (m Model) loadShelfCompareCmd(e model.ShelfEntry) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	root := m.currentWorktree
	body := m.diffBodyRows()
	tag := "shelf:" + e.ID
	v := &diffView{title: e.Origin.Path, context: "shelf #" + shortShelf(e) + " → working tree", rev: "", partial: m.diffPartial, long: m.diffLong}
	v.width, _ = m.overlayDims()
	entryID := e.ID
	full := filepath.Join(root, e.Origin.Path)

	return func() tea.Msg {
		oldSrc := func(ctx context.Context) ([]byte, error) { return svc.ShelfBlob(ctx, entryID) }
		var newSrc domain.ByteSource
		switch st, err := os.Stat(full); {
		case err == nil && st.Size() > domain.MaxDiffBytes:
			v.tooLarge = true
			return diffMsg{tag: tag, view: v}
		case err == nil:
			newSrc = func(ctx context.Context) ([]byte, error) {
				b, rerr := os.ReadFile(full)
				if rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
					return nil, rerr
				}
				return b, nil // ErrNotExist ⇒ nil ⇒ absent in the working tree
			}
		case !errors.Is(err, fs.ErrNotExist):
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		out, err := differ.Diff(context.Background(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}
