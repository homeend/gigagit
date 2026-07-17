package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// bookmarkPopup is the centered quick-switcher: a type-to-filter list of the
// repo's bookmarks.
type bookmarkPopup struct {
	popupMax
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
	popupMax
	origin string
	data   []byte
	dest   textfield
}

// bookmarkDisplay builds "<container> / <commit-or-state> / <path>".
func bookmarkDisplay(b model.Bookmark) string {
	s := b.Address().Display()
	if b.IsCommit() && b.Label != "" { // a commit bookmark carries its subject as the title
		s += " — " + b.Label
	}
	return s
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
	if m.opsIdle() && m.bookmarkSwitcher() == nil {
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

// render composites the switcher box over `below` (the overlay-stack contract).
func (p *bookmarkPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), m.renderBookmarkPopupBox(p), w, h)
}

func (m Model) renderBookmarkPopupBox(p *bookmarkPopup) string {
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)

	header := i18n.T("Bookmarks")
	if p.compareRef != nil {
		header = i18n.T("Compare %s against:", p.compareRef.Path)
	}
	if p.filtering {
		header += "  /" + p.filter + "█"
	} else if p.filter != "" {
		header += "  /" + p.filter
	}

	vis := p.visibleIdx()
	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight(i18n.T("  (none)"), textW)}
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
		capRows := popupResolveRowCap(p.maximized, termH, 12)
		h := len(vis)
		if h > capRows {
			h = capRows
		}
		bodyLines = renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	}

	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	// Wrap the hint so [z] mode / [esc] close survive on a narrow terminal,
	// where a single-line footer would truncate them off (mirrors shelfPopup).
	hint := []string{i18n.T("[?] keys"), i18n.T("[enter] jump"), i18n.T("[e] editor"), i18n.T("[p] paste"), i18n.T("[t] temp dir"), i18n.T("[a] cherry-pick"), i18n.T("[y] copy"), i18n.T("[m] mark/compare"), i18n.T("[x] remove"), i18n.T("[c] vs shelf"), i18n.T("[/] filter"), i18n.T("[z] mode"), i18n.T("[ctrl+t] full"), i18n.T("[esc] close")}
	parts = append(parts, "")
	parts = append(parts, wrapParts(hint, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// selected returns the bookmark under the popup cursor.
func (p *bookmarkPopup) selected() (model.Bookmark, bool) {
	vis := p.visibleIdx()
	if p.sel < 0 || p.sel >= len(vis) {
		return model.Bookmark{}, false
	}
	return p.items[vis[p.sel]], true
}

func (p *bookmarkPopup) byID(id string) (model.Bookmark, bool) {
	for _, b := range p.items {
		if b.ID == id {
			return b, true
		}
	}
	return model.Bookmark{}, false
}

// update handles one key while the switcher is open (the overlay contract). The
// popup is navigation-first (letters are actions, matching every other gg list);
// `/` enters a filter sub-mode where runes type a query until esc/enter.

func (p *bookmarkPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.running {
		// B1: the switcher stays visible during a paste/restore WriteFile op but
		// must be inert — a keypress here must not launch a second op.
		return m, nil
	}
	if p.filtering {
		if nm, nq, handled, commit := m.recallUpdate(scopeBookmark, msg, p.filter); handled {
			m = nm
			p.filter, p.sel = nq, 0
			if commit {
				p.filtering = false
				return m.recordSearch(scopeBookmark, p.filter)
			}
			return m, nil
		} else {
			m = nm
		}
		// Arrows/pages move the selection live while typing (no cursor reset),
		// like the commit filter; j/k stay query text.
		if filterMotion(msg, p.moveSel, popupFilterPage) {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEsc:
			p.filtering, p.filter, p.sel = false, "", 0
		case tea.KeyEnter:
			p.filtering = false // keep the filter, leave input mode
			return m.recordSearch(scopeBookmark, p.filter)
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
		m.pickGen++ // invalidate an in-flight cherry-pick probe
		m = m.popLayer()
	case tea.KeyEnter:
		if p.compareRef != nil {
			b, ok := p.selected()
			if !ok {
				return m, nil
			}
			if b.IsCommit() {
				m.statusMsg = i18n.T("cannot compare a file against a commit bookmark")
				return m, nil
			}
			return m.openCompareFocusedVsBookmark(*p.compareRef, p.compareLabel, b)
		}
		if b, ok := p.selected(); ok && b.IsCommit() {
			return m.compareCommitBookmark(b)
		}
		return m.bookmarkJump()
	case tea.KeyUp:
		p.moveSel(-1)
	case tea.KeyDown:
		p.moveSel(1)
	case tea.KeyPgUp:
		p.moveSel(-popupFilterPage)
	case tea.KeyPgDown:
		p.moveSel(popupFilterPage)
	case tea.KeyRunes:
		switch msg.String() {
		case "?":
			// Open the compact cheat sheet over the still-open switcher; esc
			// closes it and returns here (contentPopup's esc just nils itself).
			m = m.pushLayer(newContentPopup(bookmarkSwitcherHelpTitle(), bookmarkSwitcherHelp(p.compareRef != nil)))
			return m, nil
		case "/":
			p.filtering = true
			m = m.recallReset()
		case "k":
			p.moveSel(-1)
		case "j":
			p.moveSel(1)
		case "x":
			if p.compareRef != nil {
				return m, nil
			}
			return m.bookmarkRemovePrompt()
		case "p":
			if p.compareRef != nil {
				return m, nil
			}
			if mm, yes := m.commitBookmarkNotice(p); yes {
				return mm, nil
			}
			return m.bookmarkPastePrompt()
		case "m":
			if p.compareRef != nil {
				return m, nil
			}
			if mm, yes := m.commitBookmarkNotice(p); yes {
				return mm, nil
			}
			return m.bookmarkMark()
		case "c":
			if p.compareRef != nil {
				return m, nil
			}
			if mm, yes := m.commitBookmarkNotice(p); yes {
				return mm, nil
			}
			b, ok := p.selected()
			if !ok {
				return m, nil
			}
			// Keep this switcher on the stack: the shelf picker is pushed on top so
			// esc in it returns here (the diff on a pick clears both via openPickerDiff).
			m.pendingCompare = &pendingCompare{ref: bookmarkToFileRef(b), label: bookmarkDisplay(b), target: compareShelf}
			return m, m.loadShelfCmd(true)
		case "e":
			if p.compareRef != nil {
				return m, nil
			}
			if mm, yes := m.commitBookmarkNotice(p); yes { // a commit pointer has no file content
				return mm, nil
			}
			b, ok := p.selected()
			if !ok {
				return m, nil
			}
			svc := m.svc
			return m, m.openInEditorCmd(b.Path, func(ctx context.Context) ([]byte, error) {
				return svc.BookmarkBytes(ctx, b)
			})
		case "t":
			if p.compareRef != nil {
				return m, nil
			}
			b, ok := p.selected()
			if !ok {
				return m, nil
			}
			// No commitBookmarkNotice guard here: exporting a commit pointer is
			// exactly what ExportBookmark handles — [t] works for both commit and
			// file bookmarks.
			return m.startTempExportBookmark(b)
		case "a":
			if p.compareRef != nil {
				return m, nil
			}
			b, ok := p.selected()
			if !ok {
				return m, nil
			}
			if !b.IsCommit() {
				m.statusMsg = i18n.T("cherry-pick: only for a commit bookmark")
				return m, nil
			}
			return m.startPickCommit(pickTarget{sha: b.Commit})
		case "y":
			if p.compareRef != nil {
				return m, nil
			}
			if mm, yes := m.commitBookmarkNotice(p); yes {
				return mm, nil
			}
			b, ok := p.selected()
			if !ok {
				return m, nil
			}
			return m.copyFilePrompt(b.Worktree, b.Path)
		}
	}
	return m, nil
}

func (p *bookmarkPopup) moveSel(d int) {
	n := p.sel + d
	if hi := len(p.visibleIdx()) - 1; n > hi {
		n = hi
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

// bookmarkRemovePrompt opens a confirm modal; removing a bookmark is cheap to
// recreate but re-finding the file in a big repo is friction, so we confirm.
func (m Model) bookmarkRemovePrompt() (Model, tea.Cmd) {
	p := m.bookmarkSwitcher()
	if p == nil {
		return m, nil
	}
	b, ok := p.selected()
	if !ok {
		return m, nil
	}
	// Leave the switcher on the overlay stack: the modal renders above it, and on
	// Cancel the switcher is revealed automatically; on Remove the reload refreshes
	// it in place (bookmarksLoadedMsg).
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "bookmark-remove",
			Prompt:  i18n.T("Remove bookmark %s?", b.Path),
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
func (m Model) bookmarkPastePrompt() (Model, tea.Cmd) {
	p := m.bookmarkSwitcher()
	if p == nil {
		return m, nil
	}
	b, ok := p.selected()
	if !ok {
		return m, nil
	}
	data, err := m.svc.BookmarkBytes(context.Background(), b)
	if err != nil {
		m.statusMsg = i18n.T("bookmark paste: %s", err.Error())
		return m, nil
	}
	// Push over the switcher (which stays beneath); esc/success returns to it.
	return m.pushLayer(&bookmarkPastePopup{origin: b.Path, data: data, dest: newTextField(restoredPath(b.Path))}), nil
}

// bookmarkMark records the first compare mark, or compares with it on the second
// press (a self-contained two-mark, independent of the panel pair-op machinery).
func (m Model) bookmarkMark() (Model, tea.Cmd) {
	p := m.bookmarkSwitcher()
	if p == nil {
		return m, nil
	}
	b, ok := p.selected()
	if !ok {
		return m, nil
	}
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
	p := m.bookmarkSwitcher()
	if p == nil {
		return m, nil
	}
	a, okA := p.byID(aID)
	b, okB := p.byID(bID)
	if !okA || !okB {
		return m, nil
	}
	width, _ := m.overlayDims()
	v := &diffView{title: a.Path + " ↔ " + b.Path, context: bookmarkDisplay(a) + " → " + bookmarkDisplay(b), loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	return m.openPickerDiff(v, "bookmark2:"+aID+":"+bID, m.loadBookmarkCompareTwoCmd(a, b))
}

// openPickerDiff hands off from a picker popup (bookmark or shelf) to a
// full-screen diff: it pushes the diff on top of the stack (the picker sits
// beneath it, so esc from the diff returns to the picker). Shared by jump,
// compare-two, and the compare-focused-vs-X paths.
func (m Model) openPickerDiff(v *diffView, tag string, load tea.Cmd) (Model, tea.Cmd) {
	m = m.pushLayer(v)
	m.diffTag = tag
	m.diffNav = diffNavNone // a picker compare has no source file list to step
	return m, load
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

func (p *bookmarkPastePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer() // back to the switcher beneath
	case tea.KeyEnter:
		dest := strings.TrimSpace(p.dest.Value())
		if dest == "" {
			return m, nil // a destination is mandatory
		}
		data := p.data
		m = m.popLayer() // back to the switcher; it stays visible during the write
		return m.startOp(engine.WriteFile{Path: dest, Data: data})
	default:
		p.dest.HandleEditKey(msg)
	}
	return m, nil
}

func (p *bookmarkPastePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString(i18n.T("Paste bookmarked file to a new path") + "\n\n")
	b.WriteString(i18n.T("from: %s  (resolved now)", p.origin) + "\n")
	b.WriteString(viewField(i18n.T("dest: "), p.dest, true, popupContentWidth(w)) + "\n\n")
	b.WriteString(i18n.T("[type] path  [enter] paste  [esc] cancel"))
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// compareCommitBookmark opens a whole-tree compare of a bookmarked commit
// (left/base) against the currently-selected Commits-panel commit (right/
// subject), closing the switcher first. A self-compare or no loaded commit is a
// notice, not a compare.
func (m Model) compareCommitBookmark(b model.Bookmark) (Model, tea.Cmd) {
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		m.statusMsg = i18n.T("no commit selected to compare against")
		return m, nil
	}
	subject := m.commits[bi].Hash
	if subject == b.Commit {
		m.statusMsg = i18n.T("select a different commit to compare against")
		return m, nil
	}
	m = m.clearLayers() // close the switcher so the files view is not drawn under it
	return m.openCompareFiles(
		model.Endpoint{Kind: model.EndpointCommit, Hash: b.Commit}, // base
		model.Endpoint{Kind: model.EndpointCommit, Hash: subject})  // subject
}

// commitBookmarkNotice sets a "not for a commit bookmark" status and reports
// true when the highlighted bookmark is a commit pointer, so the caller can
// no-op a file-only key (paste / vs-shelf / mark). File bookmarks pass through.
func (m Model) commitBookmarkNotice(p *bookmarkPopup) (Model, bool) {
	if b, ok := p.selected(); ok && b.IsCommit() {
		m.statusMsg = i18n.T("not available for a commit bookmark")
		return m, true
	}
	return m, false
}

// bookmarkJump opens a diff of the bookmark's bytes vs the current working file.
func (m Model) bookmarkJump() (Model, tea.Cmd) {
	p := m.bookmarkSwitcher()
	if p == nil {
		return m, nil
	}
	b, ok := p.selected()
	if !ok {
		return m, nil
	}
	width, _ := m.overlayDims()
	v := &diffView{title: b.Path, context: i18n.T("%s → working tree", bookmarkDisplay(b)), rev: "", loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	return m.openPickerDiff(v, "bookmark:"+b.ID, m.loadBookmarkCompareCmd(b))
}

// loadBookmarkCompareCmd diffs the bookmark's bytes (Old) against the current
// working-tree file at its path (New, nil when absent). Mirrors loadShelfCompareCmd.
func (m Model) loadBookmarkCompareCmd(bm model.Bookmark) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	root := m.currentWorktree
	body := m.diffBodyRows()
	tag := "bookmark:" + bm.ID
	v := &diffView{title: bm.Path, context: i18n.T("%s → working tree", bookmarkDisplay(bm)), rev: "", partial: m.diffPartial, long: m.diffLong}
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
