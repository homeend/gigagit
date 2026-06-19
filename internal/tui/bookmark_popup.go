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
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// bookmarkPopup is the centered quick-switcher: a type-to-filter list of the
// repo's bookmarks.
type bookmarkPopup struct {
	items     []model.Bookmark
	rows      []string // display strings, parallel to items
	sel       int
	filter    string
	filtering bool     // true while `/` filter sub-mode captures runes
	markID    string   // first mark for a two-bookmark compare ("" = none)
	mode      dispMode // text display mode; z cycles (cutoff default)
	hscroll   int      // modeScroll horizontal offset

	compareRef   *model.FileRef // non-nil → compare mode (enter diffs against the highlighted bookmark)
	compareLabel string         // human label for the focused side, shown in the header
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

// openBookmarkSwitcher opens the global bookmark quick-switcher. It is wired
// into every navigable window (panels, file tree, diff, history, blame, stash)
// so `g` works everywhere; once the popup is open its render and key routing
// are hoisted above the content surfaces (see render() and Update()).
func (m Model) openBookmarkSwitcher() (Model, tea.Cmd) {
	if m.opsIdle() && m.bookmarkPopup == nil {
		return m, m.loadBookmarksCmd()
	}
	return m, nil
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
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)

	header := "Bookmarks"
	if p.compareRef != nil {
		header = "Compare " + p.compareRef.Path + " against:"
	}
	if p.filtering {
		header += "  /" + p.filter + "█"
	} else if p.filter != "" {
		header += "  /" + p.filter
	}

	vis := p.visibleIdx()
	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight("  (none)", textW)}
	} else {
		wr := make([]winRow, len(vis))
		for n, i := range vis {
			prefix := "  "
			var st lipgloss.Style
			if n == p.sel {
				prefix, st = "> ", selectedRow
			}
			mark := " "
			if p.items[i].ID == p.markID {
				mark = "•"
			}
			wr[n] = winRow{text: prefix + mark + " " + p.rows[i], style: st}
		}
		h := len(vis)
		if h > 12 {
			h = 12
		}
		bodyLines = renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	}

	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "", "[enter] jump  [p] paste  [m] mark/compare  [x] remove  [/] filter  [z] mode  [esc] close")
	return popupBox(inner, strings.Join(parts, "\n"))
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

// updateBookmarkPopupKey handles one key while the switcher is open. The popup
// is navigation-first (letters are actions, matching every other gg list); `/`
// enters a filter sub-mode where runes type a query until esc/enter.
func (m Model) updateBookmarkPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.bookmarkPopup
	if p.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			p.filtering, p.filter, p.sel = false, "", 0
		case tea.KeyEnter:
			p.filtering = false // keep the filter, leave input mode
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.filter); len(r) > 0 {
				p.filter = string(r[:len(r)-1])
				p.sel = 0
			}
		case tea.KeyRunes:
			p.filter += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
	// Display-mode + pan keys take precedence over the navigation switch and
	// only act in navigation mode (while filtering they are query characters).
	switch msg.String() {
	case "z":
		p.mode = p.mode.next()
		p.hscroll = 0
		return m, nil
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.bookmarkPopup = nil
	case tea.KeyEnter:
		if p.compareRef != nil {
			b, ok := m.selectedBookmark()
			if !ok {
				return m, nil
			}
			return m.openCompareFocusedVsBookmark(*p.compareRef, p.compareLabel, b)
		}
		return m.bookmarkJump()
	case tea.KeyUp:
		m.bookmarkMoveSel(-1)
	case tea.KeyDown:
		m.bookmarkMoveSel(1)
	case tea.KeyRunes:
		switch msg.String() {
		case "/":
			p.filtering = true
		case "k":
			m.bookmarkMoveSel(-1)
		case "j":
			m.bookmarkMoveSel(1)
		case "x":
			if p.compareRef != nil {
				return m, nil
			}
			return m.bookmarkRemovePrompt()
		case "p":
			if p.compareRef != nil {
				return m, nil
			}
			return m.bookmarkPastePrompt()
		case "m":
			if p.compareRef != nil {
				return m, nil
			}
			return m.bookmarkMark()
		}
	}
	return m, nil
}

func (m Model) bookmarkMoveSel(d int) {
	p := m.bookmarkPopup
	if n := p.sel + d; n >= 0 && n < len(p.visibleIdx()) {
		p.sel = n
	}
}

// bookmarkRemovePrompt opens a confirm modal; removing a bookmark is cheap to
// recreate but re-finding the file in a big repo is friction, so we confirm.
func (m Model) bookmarkRemovePrompt() (tea.Model, tea.Cmd) {
	b, ok := m.selectedBookmark()
	if !ok {
		return m, nil
	}
	m.bookmarkPopup = nil
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "bookmark-remove",
			Prompt:  "Remove bookmark " + b.Path + "?",
			Options: []string{"Remove", "Cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "Remove" {
				return m, m.bookmarkRemoveCmd(b.ID)
			}
			return m, nil
		},
	}
	return m, nil
}

func (m Model) bookmarkRemoveCmd(id string) tea.Cmd {
	svc := m.svc
	reopen := m.loadBookmarksCmd()
	return func() tea.Msg {
		if err := svc.BookmarkRemove(context.Background(), id); err != nil {
			return bookmarksLoadedMsg{err: err}
		}
		return reopen()
	}
}

// bookmarkPastePrompt fetches the bookmark's bytes, then opens the mandatory-dest
// path popup that runs engine.WriteFile on submit.
func (m Model) bookmarkPastePrompt() (tea.Model, tea.Cmd) {
	b, ok := m.selectedBookmark()
	if !ok {
		return m, nil
	}
	data, err := m.svc.BookmarkBytes(context.Background(), b)
	if err != nil {
		m.statusMsg = "bookmark paste: " + err.Error()
		return m, nil
	}
	m.bookmarkPopup = nil
	m.bookmarkPastePopup = &bookmarkPastePopup{origin: b.Path, data: data}
	return m, nil
}

// bookmarkMark records the first compare mark, or compares with it on the second
// press (a self-contained two-mark, independent of the panel pair-op machinery).
func (m Model) bookmarkMark() (tea.Model, tea.Cmd) {
	b, ok := m.selectedBookmark()
	if !ok {
		return m, nil
	}
	p := m.bookmarkPopup
	if p.markID == "" || p.markID == b.ID {
		if p.markID == b.ID {
			p.markID = "" // toggle off
		} else {
			p.markID = b.ID
		}
		return m, nil
	}
	return m.openBookmarkCompareTwo(p.markID, b.ID)
}

// openBookmarkCompareTwo diffs two bookmarks (marked = old, selected = new),
// both resolved via BookmarkBytes.
func (m Model) openBookmarkCompareTwo(aID, bID string) (Model, tea.Cmd) {
	a, okA := m.bookmarkByID(aID)
	b, okB := m.bookmarkByID(bID)
	if !okA || !okB {
		return m, nil
	}
	m.bookmarkPopup = nil
	width, _ := m.overlayDims()
	m.diffView = &diffView{title: a.Path + " ↔ " + b.Path, context: bookmarkDisplay(a) + " → " + bookmarkDisplay(b), loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	m.diffTag = "bookmark2:" + aID + ":" + bID
	return m, m.loadBookmarkCompareTwoCmd(a, b)
}

func (m Model) loadBookmarkCompareTwoCmd(a, b model.Bookmark) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	tag := "bookmark2:" + a.ID + ":" + b.ID
	v := &diffView{title: a.Path + " ↔ " + b.Path, context: bookmarkDisplay(a) + " → " + bookmarkDisplay(b), partial: m.diffPartial, long: m.diffLong}
	v.width, _ = m.overlayDims()
	return func() tea.Msg {
		oldSrc := func(ctx context.Context) ([]byte, error) { return svc.BookmarkBytes(ctx, a) }
		newSrc := func(ctx context.Context) ([]byte, error) { return svc.BookmarkBytes(ctx, b) }
		out, err := differ.Diff(context.Background(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}

// --- paste destination popup ---------------------------------------------

func (m Model) updateBookmarkPasteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.bookmarkPastePopup
	switch msg.Type {
	case tea.KeyEsc:
		m.bookmarkPastePopup = nil
	case tea.KeyEnter:
		dest := strings.TrimSpace(p.dest)
		if dest == "" {
			return m, nil // a destination is mandatory
		}
		data := p.data
		m.bookmarkPastePopup = nil
		return m.startOp(engine.WriteFile{Path: dest, Data: data})
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

func (m Model) renderBookmarkPastePopup() string {
	p := m.bookmarkPastePopup
	var b strings.Builder
	b.WriteString("Paste bookmarked file to a new path\n\n")
	b.WriteString("from: " + p.origin + "  (resolved now)\n")
	b.WriteString("dest: " + p.dest + "\n\n")
	b.WriteString("[type] path  [enter] paste  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
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
