package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/model"
)

// bookmarkPopup is the centered quick-switcher: a type-to-filter list of the
// repo's bookmarks.
type bookmarkPopup struct {
	items  []model.Bookmark
	rows   []string // display strings, parallel to items
	sel    int
	filter string
	markID string // first mark for a two-bookmark compare ("" = none)
}

// bookmarkPastePopup collects the (mandatory, no-default) paste destination,
// carrying the already-resolved bytes so Enter just writes them.
type bookmarkPastePopup struct {
	origin string
	data   []byte
	dest   string
}

// bookmarkDisplay builds "<container> / <commit-or-state> / <path>".
func bookmarkDisplay(b model.Bookmark) string {
	container := "?"
	switch b.State {
	case model.StateCommitted:
		container = b.Branch
		if container == "" {
			container = "commit"
		}
	case model.StateShelf:
		container = "shelf"
	default:
		container = "wt:" + filepath.Base(b.Worktree)
	}
	mid := b.State.String()
	if b.State == model.StateCommitted && len(b.Commit) >= 7 {
		mid = b.Commit[:7]
	}
	return fmt.Sprintf("%s / %s / %s", container, mid, b.Path)
}

type bookmarksLoadedMsg struct {
	items []model.Bookmark
	err   error
}

func (m Model) loadBookmarksCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		bs, err := svc.BookmarkList(context.Background(), 0, 0)
		return bookmarksLoadedMsg{items: bs, err: err}
	}
}

func newBookmarkPopup(items []model.Bookmark) *bookmarkPopup {
	p := &bookmarkPopup{items: items}
	for _, b := range items {
		p.rows = append(p.rows, bookmarkDisplay(b))
	}
	return p
}

// visibleIdx returns item indices matching the filter (case-insensitive).
func (p *bookmarkPopup) visibleIdx() []int {
	var idx []int
	q := strings.ToLower(p.filter)
	for i, row := range p.rows {
		if q == "" || strings.Contains(strings.ToLower(row), q) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m Model) renderBookmarkPopup() string {
	p := m.bookmarkPopup
	var b strings.Builder
	b.WriteString("Bookmarks  (type to filter)\n\n")
	vis := p.visibleIdx()
	if len(vis) == 0 {
		b.WriteString("  (none)\n")
	}
	for n, i := range vis {
		cursor := "  "
		if n == p.sel {
			cursor = "> "
		}
		mark := " "
		if p.items[i].ID == p.markID {
			mark = "•"
		}
		b.WriteString(cursor + mark + " " + p.rows[i] + "\n")
	}
	if p.filter != "" {
		b.WriteString("\nfilter: " + p.filter)
	}
	b.WriteString("\n\n[↑↓] move  [enter] jump  [p] paste  [m] mark/compare  [x] remove  [esc] close")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}

// selectedBookmark returns the bookmark under the popup cursor.
func (m Model) selectedBookmark() (model.Bookmark, bool) {
	p := m.bookmarkPopup
	vis := p.visibleIdx()
	if p.sel < 0 || p.sel >= len(vis) {
		return model.Bookmark{}, false
	}
	return p.items[vis[p.sel]], true
}

func (m Model) bookmarkByID(id string) (model.Bookmark, bool) {
	for _, b := range m.bookmarkPopup.items {
		if b.ID == id {
			return b, true
		}
	}
	return model.Bookmark{}, false
}

// updateBookmarkPopupKey handles one key while the switcher is open.
func (m Model) updateBookmarkPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.bookmarkPopup
	switch msg.Type {
	case tea.KeyEsc:
		m.bookmarkPopup = nil
	case tea.KeyEnter:
		return m.bookmarkJump()
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
	case tea.KeyDown:
		if p.sel < len(p.visibleIdx())-1 {
			p.sel++
		}
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.filter); len(r) > 0 {
			p.filter = string(r[:len(r)-1])
			p.sel = 0
		}
	case tea.KeyRunes:
		return m.bookmarkPopupRune(msg.String())
	}
	return m, nil
}

// bookmarkPopupRune routes action runes (p/x/m) before falling back to filtering.
// p/x/m are implemented in the paste/remove/compare task; here they fall through
// to the filter so the popup is usable.
func (m Model) bookmarkPopupRune(s string) (tea.Model, tea.Cmd) {
	m.bookmarkPopup.filter += s
	m.bookmarkPopup.sel = 0
	return m, nil
}

// bookmarkJump opens a diff of the bookmark's bytes vs the current working file.
func (m Model) bookmarkJump() (tea.Model, tea.Cmd) {
	b, ok := m.selectedBookmark()
	if !ok {
		return m, nil
	}
	m.bookmarkPopup = nil
	width, _ := m.overlayDims()
	m.diffView = &diffView{title: b.Path, context: bookmarkDisplay(b) + " → working tree", rev: "", loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	m.diffTag = "bookmark:" + b.ID
	return m, m.loadBookmarkCompareCmd(b)
}

// loadBookmarkCompareCmd diffs the bookmark's bytes (Old) against the current
// working-tree file at its path (New, nil when absent). Mirrors loadShelfCompareCmd.
func (m Model) loadBookmarkCompareCmd(bm model.Bookmark) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	root := m.currentWorktree
	body := m.diffBodyRows()
	tag := "bookmark:" + bm.ID
	v := &diffView{title: bm.Path, context: bookmarkDisplay(bm) + " → working tree", rev: "", partial: m.diffPartial, long: m.diffLong}
	v.width, _ = m.overlayDims()
	full := filepath.Join(root, bm.Path)

	return func() tea.Msg {
		oldSrc := func(ctx context.Context) ([]byte, error) { return svc.BookmarkBytes(ctx, bm) }
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
				return b, nil
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
