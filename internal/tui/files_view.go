package tui

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/model"
)

// commitFileLines renders a commit's changed files as content lines:
// root-level files first (no heading), then one bold heading per directory
// (its full path) with the directory's files indented beneath. Exactly one
// heading level — no nesting. Sorting is dir-major because a plain path sort
// interleaves a directory's files with its subdirectories, which would emit
// the same heading twice.
func commitFileLines(files []model.CommitFile) []contentLine {
	if len(files) == 0 {
		return []contentLine{{text: "(no files)"}}
	}
	sorted := make([]model.CommitFile, len(files))
	copy(sorted, files)
	sort.SliceStable(sorted, func(a, b int) bool {
		da, db := path.Dir(sorted[a].Path), path.Dir(sorted[b].Path)
		if da != db {
			return da < db // "." sorts before any directory name
		}
		return sorted[a].Path < sorted[b].Path
	})

	out := make([]contentLine, 0, len(sorted))
	lastDir := ""
	for _, f := range sorted {
		dir := path.Dir(f.Path)
		if dir == "." {
			out = append(out, contentLine{text: fileLine(f), path: f.Path, oldPath: f.OldPath, status: f.Status})
			continue
		}
		if dir != lastDir {
			out = append(out, contentLine{text: dir + "/", heading: true})
			lastDir = dir
		}
		out = append(out, contentLine{text: "  " + fileLine(f), path: f.Path, oldPath: f.OldPath, status: f.Status})
	}
	return out
}

// fileLine renders one file row: "<letter>  <basename>"; renames show the
// full old path and the new basename.
func fileLine(f model.CommitFile) string {
	if f.OldPath != "" {
		return f.Status + "  " + f.OldPath + " → " + path.Base(f.Path)
	}
	return f.Status + "  " + path.Base(f.Path)
}

// commitFilesMsg carries one commit's changed files, tagged with the hash so
// stale results from fast j/k movement can be dropped.
type commitFilesMsg struct {
	hash    string
	subject string
	files   []model.CommitFile
	err     error
}

// loadCommitFilesCmd fetches the changed files of commit c off the UI thread.
func (m Model) loadCommitFilesCmd(c model.Commit) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		files, err := svc.CommitFiles(context.Background(), c.Hash)
		return commitFilesMsg{hash: c.Hash, subject: c.Subject, files: files, err: err}
	}
}

// treeFilesMsg carries the full file tree of commit hash, with the content lines
// already built (the dir-major sort runs off the UI thread — the tree can be
// 10^4–10^5 files on a large repo).
type treeFilesMsg struct {
	hash    string
	subject string
	lines   []contentLine
	err     error
}

// loadTreeFilesCmd fetches every file in commit c's tree (ls-tree) AND builds the
// content lines off the UI thread, so the render thread only assigns the result.
func (m Model) loadTreeFilesCmd(c model.Commit) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		files, err := svc.TreeFiles(context.Background(), c.Hash)
		if err != nil {
			return treeFilesMsg{hash: c.Hash, subject: c.Subject, err: err}
		}
		return treeFilesMsg{hash: c.Hash, subject: c.Subject, lines: commitFileLines(files)}
	}
}

// loadFilesForCmd loads the files-view content for commit c in the active mode:
// the full tree (every file at the commit) when filesAllFiles, else the changed
// set. Used wherever the view (re)loads so the mode sticks across navigation.
func (m Model) loadFilesForCmd(c model.Commit) tea.Cmd {
	if m.filesAllFiles {
		return m.loadTreeFilesCmd(c)
	}
	return m.loadCommitFilesCmd(c)
}

// filesViewCommit returns the loaded commit the files view is showing (matched
// by filesHash); falls back to a hash-only Commit when it is outside the feed.
func (m Model) filesViewCommit() model.Commit {
	for i := range m.commits {
		if m.commits[i].Hash == m.filesHash {
			return m.commits[i]
		}
	}
	return model.Commit{Hash: m.filesHash}
}

// compareFilesMsg carries a whole-tree comparison's changed files, tagged so a
// superseded load (fast re-open) can be dropped.
type compareFilesMsg struct {
	tag   string
	files []model.CommitFile
	err   error
}

// openCompareFiles opens the files view in compare mode for the endpoint pair
// (left = older, right = newer), e.g. a commit vs the working tree. The proven
// single-commit path is untouched; this is a parallel mode keyed off
// filesCompare.
func (m Model) openCompareFiles(left, right model.Endpoint) (Model, tea.Cmd) {
	m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
	m.filesTitle = left.Display() + " ↔ " + right.Display()
	m.filesCompare = true
	m.filesAllFiles = false // compare mode is its own thing
	m.filesPreview = nil
	m.filesPreviewTag = ""
	m.filesLeft = left
	m.filesRight = right
	// h/b (history/blame) context: prefer a commit side; "" means working tree.
	switch {
	case right.Kind == model.EndpointCommit:
		m.filesHash = right.Hash
	case left.Kind == model.EndpointCommit:
		m.filesHash = left.Hash
	default:
		m.filesHash = ""
	}
	m.compareTag = "cmp:" + left.CacheTag() + ":" + right.CacheTag()
	// Focus the tree: compare mode has no live commit list, and moving the commit
	// selection would discard the comparison. The focus-switch keys are inert here.
	m.filesTreeFocused = true
	return m, m.loadCompareFilesCmd(left, right, m.compareTag)
}

// loadCompareFilesCmd fetches the changed-file list off the UI thread.
func (m Model) loadCompareFilesCmd(left, right model.Endpoint, tag string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		files, err := svc.CompareFiles(context.Background(), left, right)
		return compareFilesMsg{tag: tag, files: files, err: err}
	}
}

// shortHash truncates a sha to 7 characters for display.
func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// filesPageRows is the tree's visible row capacity: the left column's box
// height minus borders (2), the title line (1), and the hint line (1).
func (m Model) filesPageRows() int {
	n := m.layout().bodyH - 4
	if n < 1 {
		n = 1
	}
	return n
}

// updateFilesViewKey routes keys while the files view is open. ←/→/tab pick
// which side owns vertical movement (filesTreeFocused); the commits side
// keeps the follow-live reload; ctrl+↑/↓ always scrolls the tree; /-search,
// close keys and quit are focus-independent; everything else is swallowed.
func (m Model) updateFilesViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.filesView
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.typing { // /-input mode captures every key (same as the help window)
		switch msg.Type {
		case tea.KeyEsc:
			p.typing = false
			p.query = ""
			p.sel = 0
		case tea.KeyEnter:
			p.typing = false // commit: search stays active
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
			}
			p.sel = 0
		case tea.KeySpace:
			p.query += " "
			p.sel = 0
		case tea.KeyRunes:
			p.query += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
	switch msg.String() {
	case ".":
		return m.openActionMenu(), nil
	case "g": // global bookmark quick-switcher
		return m.openBookmarkSwitcher()
	case "G": // global shelf quick-switcher
		return m.openShelfSwitcher()
	case "z":
		if m.filesPreview != nil && !m.filesTreeFocused { // z cycles the focused preview
			m.filesPreview.mode = m.filesPreview.mode.next()
			m.filesPreview.hscroll = 0
			return m, nil
		}
		p.mode = p.mode.next()
		p.hscroll = 0
		return m, nil
	case "a": // toggle full-tree (every file at this commit) vs the changed set
		if m.stashView != nil || m.filesCompare || m.filesHash == "" {
			return m, nil // only meaningful for a commit files view
		}
		m.filesAllFiles = !m.filesAllFiles
		m.filesPreview = nil // the preview is a full-tree concept
		m.filesPreviewTag = ""
		p.lines = []contentLine{{text: "(loading…)"}}
		p.sel = 0
		p.query = ""
		m.filesReadInflight = true
		return m, m.loadFilesForCmd(m.filesViewCommit())
	// The commit-list side IS the Commits panel selection (m.focus stays
	// panelCommits), so the graph window keys mirror the base panel exactly
	// (model.go). Only on the file-tree side do shift+arrows scroll the tree.
	case ">":
		if !m.filesTreeFocused && m.filesPreview == nil && m.graphActive() {
			m.commitGraphCols = m.clampCols(m.graphCols() + m.graphStep())
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
		}
		return m, nil
	case "<":
		if !m.filesTreeFocused && m.filesPreview == nil && m.graphActive() {
			m.commitGraphCols = m.clampCols(m.graphCols() - m.graphStep())
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
		}
		return m, nil
	case "=":
		if !m.filesTreeFocused && m.filesPreview == nil && m.graphActive() {
			m = m.snapGraphToSelected()
		}
		return m, nil
	case "shift+left":
		if !m.filesTreeFocused && m.filesPreview == nil && m.graphActive() {
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll - m.graphPanStep())
			return m, nil
		}
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if !m.filesTreeFocused && m.filesPreview == nil && m.graphActive() {
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll + m.graphPanStep())
			return m, nil
		}
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
		return m, nil
	// q is inert here: only the base layout quits on q. esc is the back key;
	// ctrl+c (handled above) remains the universal quit.
	case "esc":
		if m.filesPreview != nil { // the preview is the topmost surface — close it first
			m.filesPreview = nil
			m.filesPreviewTag = ""
			return m, nil
		}
		if p.query != "" { // first esc clears the committed search
			p.query = ""
			p.sel = 0
			return m, nil
		}
		m.filesView = nil
		m.filesCompare = false
		m.compareTag = ""
		m.filesTreeFocused = false
		m.filesAllFiles = false
		return m, nil
	case "l":
		m.filesView = nil
		m.filesCompare = false
		m.compareTag = ""
		m.filesTreeFocused = false
		m.filesAllFiles = false
		m.filesPreview = nil
		m.filesPreviewTag = ""
		return m, nil
	case "/":
		if m.filesPreview != nil { // no commit filter while the preview owns the right column
			return m, nil
		}
		// Focus decides the search target. The commit-list side routes to the
		// base commit filter (which the right column already renders); the tree
		// side, and the stash list (which has no base filter), filter the tree.
		if !m.filesTreeFocused && m.stashView == nil {
			m.filterPanel = panelCommits
			m.filterQuery = ""
			m.filterTyping = true
			m.sel[panelCommits] = 0
			return m, nil
		}
		p.typing = true
		p.query = ""
		p.sel = 0
	case "h":
		if !m.filesTreeFocused {
			return m, nil
		}
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) || vis[p.sel].path == "" {
			return m, nil
		}
		ctx := navContext{path: vis[p.sel].path, rev: m.filesHash}
		hv := newHistoryView(ctx)
		m = m.pushLayer(hv)
		return m, m.loadHistoryListCmd(ctx, hv.listTag)
	case "b":
		if !m.filesTreeFocused {
			return m, nil
		}
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) || vis[p.sel].path == "" {
			return m, nil
		}
		ctx := navContext{path: vis[p.sel].path, rev: m.filesHash}
		bv := newBlameView(ctx)
		m = m.pushLayer(bv)
		return m, m.loadBlameCmd(ctx, bv.tag)
	case "enter":
		if !m.filesTreeFocused {
			// List side: for a stash, enter opens the Apply/Pop/Drop popup
			// (the list's defining verb); for commits it's a no-op.
			if v := m.stashView; v != nil && v.sel >= 0 && v.sel < len(v.entries) {
				e := v.entries[v.sel]
				m = m.pushLayer(&stashActionPopup{ref: e.Ref, subject: e.Subject})
			}
			return m, nil
		}
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) || vis[p.sel].path == "" {
			return m, nil // heading row, placeholder, or empty view
		}
		if m.width > 0 && m.width < 60 {
			m.statusMsg = "terminal too narrow for the diff view"
			return m, nil
		}
		l := vis[p.sel]
		m.diffView = &diffView{
			title:   l.path,
			context: "@ " + strings.TrimPrefix(m.filesTitle, "Files "),
			rev:     m.filesHash,
			loading: true,
			partial: m.diffPartial,
			long:    m.diffLong,
		}
		if m.filesAllFiles {
			// Full-tree mode: the file may be unchanged in this commit, so a
			// parent-diff would be empty. Diff the commit's version against the
			// working tree instead — useful for any file in the tree.
			left := model.Endpoint{Kind: model.EndpointCommit, Hash: m.filesHash}
			right := model.Endpoint{Kind: model.EndpointWorkTree}
			m.diffView.context = shortHash(m.filesHash) + " ↔ working tree"
			m.diffTag = "cmp:" + left.CacheTag() + ":" + right.CacheTag() + ":" + l.path
			return m, m.loadCompareDiffCmd(left, right, l)
		}
		if m.filesCompare {
			m.diffView.context = m.filesTitle
			m.diffTag = "cmp:" + m.filesLeft.CacheTag() + ":" + m.filesRight.CacheTag() + ":" + l.path
			return m, m.loadCompareDiffCmd(m.filesLeft, m.filesRight, l)
		}
		m.diffTag = "commit:" + m.filesHash + ":" + l.path
		return m, m.loadCommitDiffCmd(m.filesHash, l)
	case "left":
		m.filesTreeFocused = true
	case "right":
		if !m.filesCompare { // compare mode has no commit-list side to focus
			m.filesTreeFocused = false
		}
	case "tab", "shift+tab":
		if !m.filesCompare {
			m.filesTreeFocused = !m.filesTreeFocused
		}
	case "up", "k":
		if m.filesTreeFocused {
			p.move(-1)
			return m, nil
		}
		return m.moveListUnderFilesView(-1)
	case "down", "j":
		if m.filesTreeFocused {
			p.move(1)
			return m, nil
		}
		return m.moveListUnderFilesView(1)
	case "ctrl+up": // always the tree, from either side
		p.move(-1)
	case "ctrl+down":
		p.move(1)
	case "pgup":
		if m.filesTreeFocused {
			p.move(-m.filesPageRows())
			return m, nil
		}
		return m.moveListUnderFilesView(-m.pageStep())
	case "pgdown":
		if m.filesTreeFocused {
			p.move(m.filesPageRows())
			return m, nil
		}
		return m.moveListUnderFilesView(m.pageStep())
	}
	return m, nil
}

// moveListUnderFilesView moves the list side (the right column) by delta and
// fires its follow-live reload: the stash list when the file tree is showing a
// stash, otherwise the Commits list.
func (m Model) moveListUnderFilesView(delta int) (tea.Model, tea.Cmd) {
	if m.filesPreview != nil {
		// The preview owns the right column: vertical movement scrolls it instead
		// of the commit list (so filesHash can't change under a live preview).
		m.filesPreview.move(delta)
		return m, nil
	}
	if m.stashView != nil {
		return m.moveStashUnderFilesView(delta)
	}
	return m.moveCommitUnderFilesView(delta)
}

// moveCommitUnderFilesView shifts the Commits selection by delta and fires
// the follow-live reload when it lands on a different commit.
func (m Model) moveCommitUnderFilesView(delta int) (tea.Model, tea.Cmd) {
	// Compare mode has no live commit list: shifting the selection here would
	// reassign filesHash and reload a plain commit view, discarding the
	// comparison. Guard at the chokepoint so keyboard AND mouse (mouse.go calls
	// this directly) are both locked out.
	if m.filesCompare {
		return m, nil
	}
	// Pure-drop: while a per-commit files read is outstanding, ignore the move
	// entirely so held j/k is paced by read completion instead of queuing a read
	// per OS key-repeat (the files load is expensive on a large repo).
	if m.filesReadInflight {
		return m, nil
	}
	n := m.panelLen(panelCommits)
	s := m.sel[panelCommits] + delta
	if s > n-1 {
		s = n - 1
	}
	if s < 0 {
		s = 0
	}
	if s == m.sel[panelCommits] {
		return m, nil
	}
	m.sel[panelCommits] = s
	bi, ok := m.backingIndex(panelCommits)
	if !ok || m.commits[bi].Hash == m.filesHash {
		return m.maybeLoadMoreCommits() // nil cmd when not needed
	}
	m.filesHash = m.commits[bi].Hash
	m.filesReadInflight = true
	filesCmd := m.loadFilesForCmd(m.commits[bi])
	m, more := m.maybeLoadMoreCommits()
	if more != nil {
		return m, tea.Batch(filesCmd, more)
	}
	return m, filesCmd
}

// syncFilesViewToSelectedCommit reloads the tree for the currently selected
// commit when it differs from the one on display. Called when a commit filter
// commits with the files view open, so the tree follows the narrowed selection
// without the user having to press j/k.
func (m Model) syncFilesViewToSelectedCommit() (tea.Model, tea.Cmd) {
	if m.filesCompare {
		return m, nil // never follow-reload in compare mode (input-agnostic lock)
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok || m.commits[bi].Hash == m.filesHash {
		return m, nil
	}
	m.filesHash = m.commits[bi].Hash
	m.filesReadInflight = true
	return m, m.loadFilesForCmd(m.commits[bi])
}

// renderFilesView draws the commit files tree as one full-height left-column
// box; it replaces the Branches/Worktrees/Status panels while open. The border
// follows filesTreeFocused; the Commits panel blurs via panelFocused while the
// tree side is active.
func (m Model) renderFilesView(boxW, boxH int) string {
	p := m.filesView
	contentH := boxH - 2 // top/bottom border
	if contentH < 1 {
		contentH = 1
	}
	innerW := boxW - 4 // border (2) + horizontal padding (2)
	if innerW < 1 {
		innerW = 1
	}
	// The /-search input rides its own line beneath the title (not appended to
	// it) so a long commit subject can't truncate the query out of view.
	search := p.searchLine()
	rowsCap := contentH - 2 // title + hint lines
	if search != "" {
		rowsCap-- // the search line claims one more row
	}
	if rowsCap < 1 {
		rowsCap = 1
	}

	vis := p.visible()
	// Window-then-build: on a full tree the list is 10^4–10^5 rows, and building a
	// winRow for every row each frame is O(n) (≈0.5s at 40k rows). For the
	// 1-line-per-row modes (cutoff/scroll), only the slice the window can show is
	// built; the math is byte-identical to building all rows (windowStart clamps
	// the same window either way). Wrap mode has variable row height, so it keeps
	// the full build (wrapping a 10^5 tree is a deliberate, rare choice).
	s0, s1, anchor := 0, len(vis), p.sel
	if p.mode != modeWrap && len(vis) > 2*rowsCap+1 {
		if s0 = p.sel - rowsCap; s0 < 0 {
			s0 = 0
		}
		if s1 = p.sel + rowsCap + 1; s1 > len(vis) {
			s1 = len(vis)
		}
		anchor = p.sel - s0
	}
	window := vis[s0:s1]
	wr := make([]winRow, len(window))
	for i, l := range window {
		prefix := "  "
		var st lipgloss.Style
		switch {
		case i == anchor:
			// Cursor highlight wins over heading style so the cursor stays
			// visible when it rests on a heading row.
			prefix = "> "
			st = selectedRow
		case l.heading:
			prefix = ""
			st = titleStyle
		}
		text := l.text
		// Directory headings carry the full path; a leaf dir (e.g. .../v3/ApiObject/)
		// would tail-truncate to look identical to its parent (.../v3/). Elide from
		// the left in cutoff mode so the distinguishing leaf stays visible; wrap and
		// scroll modes already show the whole row, and the selected row's reveal
		// shows the untrimmed path. Pre-elided to fit so renderWindow won't re-cut it.
		if l.heading && p.mode == modeCutoff {
			text = elideLeft(l.text, innerW-lipgloss.Width(prefix))
		}
		wr[i] = winRow{text: prefix + text, style: st}
	}

	lines := make([]string, 0, contentH)
	lines = append(lines, padRight(truncate(m.filesTitle, innerW), innerW))
	if search != "" {
		lines = append(lines, padRight(truncate(search, innerW), innerW))
	}
	if len(vis) == 0 {
		lines = append(lines, padRight(truncate("  (no match)", innerW), innerW))
	} else {
		win := renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: p.mode, anchor: anchor, hscroll: p.hscroll})
		lines = append(lines, win...)
	}
	for len(lines) < contentH-1 {
		lines = append(lines, padRight("", innerW))
	}
	hint := "[enter] diff  [h] history  [b] blame  [/] search  [esc] close"
	if len(vis) > rowsCap {
		hint = fmt.Sprintf("%d/%d  %s", p.sel+1, len(vis), hint)
	}
	lines = append(lines, padRight(truncate(hint, innerW), innerW))

	style := bluredPanel
	if m.filesTreeFocused {
		style = focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}
