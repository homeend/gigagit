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
	// q is inert here: only the base layout quits on q. esc is the back key;
	// ctrl+c (handled above) remains the universal quit.
	case "esc":
		if p.query != "" { // first esc clears the committed search
			p.query = ""
			p.sel = 0
			return m, nil
		}
		m.filesView = nil
		m.filesTreeFocused = false
		return m, nil
	case "l":
		m.filesView = nil
		m.filesTreeFocused = false
		return m, nil
	case "/":
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
		m = m.pushSurface(hv)
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
		m = m.pushSurface(bv)
		return m, m.loadBlameCmd(ctx, bv.tag)
	case "enter":
		if !m.filesTreeFocused {
			// List side: for a stash, enter opens the Apply/Pop/Drop popup
			// (the list's defining verb); for commits it's a no-op.
			if v := m.stashView; v != nil && v.sel >= 0 && v.sel < len(v.entries) {
				e := v.entries[v.sel]
				m.stashAction = &stashActionPopup{ref: e.Ref, subject: e.Subject}
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
		m.diffTag = "commit:" + m.filesHash + ":" + l.path
		return m, m.loadCommitDiffCmd(m.filesHash, l)
	case "left":
		m.filesTreeFocused = true
	case "right":
		m.filesTreeFocused = false
	case "tab", "shift+tab":
		m.filesTreeFocused = !m.filesTreeFocused
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
	if m.stashView != nil {
		return m.moveStashUnderFilesView(delta)
	}
	return m.moveCommitUnderFilesView(delta)
}

// moveCommitUnderFilesView shifts the Commits selection by delta and fires
// the follow-live reload when it lands on a different commit.
func (m Model) moveCommitUnderFilesView(delta int) (tea.Model, tea.Cmd) {
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
		return m, m.maybeLoadMoreCommits() // nil when not needed
	}
	m.filesHash = m.commits[bi].Hash
	filesCmd := m.loadCommitFilesCmd(m.commits[bi])
	if more := m.maybeLoadMoreCommits(); more != nil {
		return m, tea.Batch(filesCmd, more)
	}
	return m, filesCmd
}

// syncFilesViewToSelectedCommit reloads the tree for the currently selected
// commit when it differs from the one on display. Called when a commit filter
// commits with the files view open, so the tree follows the narrowed selection
// without the user having to press j/k.
func (m Model) syncFilesViewToSelectedCommit() (tea.Model, tea.Cmd) {
	bi, ok := m.backingIndex(panelCommits)
	if !ok || m.commits[bi].Hash == m.filesHash {
		return m, nil
	}
	m.filesHash = m.commits[bi].Hash
	return m, m.loadCommitFilesCmd(m.commits[bi])
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
	rowsCap := contentH - 2 // title + hint lines
	if rowsCap < 1 {
		rowsCap = 1
	}

	title := m.filesTitle
	if p.typing {
		title += " /" + p.query + "█"
	} else if p.query != "" {
		title += " /" + p.query
	}

	vis := p.visible()
	wr := make([]winRow, len(vis))
	for i, l := range vis {
		prefix := "  "
		var st lipgloss.Style
		switch {
		case i == p.sel:
			// Cursor highlight wins over heading style so the cursor stays
			// visible when it rests on a heading row.
			prefix = "> "
			st = selectedRow
		case l.heading:
			prefix = ""
			st = titleStyle
		}
		wr[i] = winRow{text: prefix + l.text, style: st}
	}

	lines := make([]string, 0, contentH)
	lines = append(lines, padRight(truncate(title, innerW), innerW))
	if len(vis) == 0 {
		lines = append(lines, padRight(truncate("  (no match)", innerW), innerW))
	} else {
		win := renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
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
