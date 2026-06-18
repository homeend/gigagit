package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// selectedShelfEntry returns the entry under the Shelf-tab cursor.
func (m Model) selectedShelfEntry() (model.ShelfEntry, bool) {
	if m.focus != panelShelf {
		return model.ShelfEntry{}, false
	}
	bi, ok := m.backingIndex(panelShelf)
	if !ok || bi < 0 || bi >= len(m.shelfEntries) {
		return model.ShelfEntry{}, false
	}
	return m.shelfEntries[bi], true
}

func (m Model) canShelfRestore() bool { _, ok := m.selectedShelfEntry(); return ok }
func (m Model) canShelfRemove() bool  { _, ok := m.selectedShelfEntry(); return ok }
func (m Model) canShelfCompare() bool { _, ok := m.selectedShelfEntry(); return ok }

// shelfTabRows are the Shelf-tab . menu actions (restore / remove), present
// only when a shelf entry is selected.
func (m Model) shelfTabRows() []actionRow {
	e, ok := m.selectedShelfEntry()
	if !ok {
		return nil
	}
	return []actionRow{
		{
			id:    "shelf-restore",
			label: "Restore to…",
			run: func(m Model) (tea.Model, tea.Cmd) {
				m.shelfRestorePopup = &shelfRestorePopup{entryID: e.ID, origin: e.Path}
				return m, nil
			},
		},
		{
			id:    "shelf-remove",
			label: "Remove from shelf",
			run: func(m Model) (tea.Model, tea.Cmd) {
				m.modal = &decisionState{
					req: engine.DecisionRequest{
						ID:      "shelf-remove",
						Prompt:  "Remove " + e.Path + " from the shelf? (the frozen copy is destroyed)",
						Options: []string{"Remove", "Cancel"},
					},
					onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
						if opt == "Remove" {
							return m, m.shelfRemoveCmd(e.ID)
						}
						return m, nil
					},
				}
				return m, nil
			},
		},
	}
}

// shelfRemoveCmd removes an entry then reloads the tab.
func (m Model) shelfRemoveCmd(entryID string) tea.Cmd {
	svc := m.svc
	reload := m.loadShelfCmd()
	return func() tea.Msg {
		if err := svc.ShelfRemove(context.Background(), entryID); err != nil {
			return shelfLoadedMsg{err: err}
		}
		return reload()
	}
}

// --- restore destination popup -------------------------------------------

// shelfRestorePopup collects the (mandatory, no-default) restore destination.
type shelfRestorePopup struct {
	entryID string
	origin  string // origin path, shown only as a hint — NOT prefilled
	dest    string // typed destination (starts empty)
}

// updateShelfRestoreKey handles one key while the restore popup is open.
func (m Model) updateShelfRestoreKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.shelfRestorePopup
	switch msg.Type {
	case tea.KeyEsc:
		m.shelfRestorePopup = nil
	case tea.KeyEnter:
		dest := strings.TrimSpace(p.dest)
		if dest == "" {
			return m, nil // a destination is mandatory
		}
		entry := p.entryID
		m.shelfRestorePopup = nil
		blob, err := m.svc.ShelfBlob(context.Background(), entry)
		if err != nil {
			m.statusMsg = "shelf restore: " + err.Error()
			return m, nil
		}
		// engine.WriteFile owns the Overwrite/Cancel fork via the modal decider.
		return m.startOp(engine.WriteFile{Path: dest, Data: blob})
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.dest); len(r) > 0 {
			p.dest = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		p.dest += " "
	case tea.KeyRunes:
		p.dest += string(msg.Runes)
	}
	return m, nil
}

// renderShelfRestorePopup draws the restore-destination dialog.
func (m Model) renderShelfRestorePopup() string {
	p := m.shelfRestorePopup
	var b strings.Builder
	b.WriteString("Restore shelved file to a new path\n\n")
	b.WriteString("from: " + p.origin + "  (shelved copy)\n")
	b.WriteString("dest: " + p.dest + "\n\n")
	b.WriteString("[type] path  [enter] restore  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}

// --- compare (entry vs working tree) -------------------------------------

// openShelfCompare opens a diff of the selected entry (old) against the current
// working-tree version of its origin path (new).
func (m Model) openShelfCompare() (Model, tea.Cmd) {
	e, ok := m.selectedShelfEntry()
	if !ok {
		return m, nil
	}
	width, _ := m.overlayDims()
	m.diffView = &diffView{title: e.Path, context: "shelf #" + shortShelf(e) + " → working tree", rev: "", loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	m.diffTag = "shelf:" + e.ID
	return m, m.loadShelfCompareCmd(e)
}

func shortShelf(e model.ShelfEntry) string {
	if len(e.SHA) > 8 {
		return e.SHA[:8]
	}
	return e.SHA
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
	v := &diffView{title: e.Path, context: "shelf #" + shortShelf(e) + " → working tree", rev: "", partial: m.diffPartial, long: m.diffLong}
	v.width, _ = m.overlayDims()
	entryID := e.ID
	full := filepath.Join(root, e.Path)

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
