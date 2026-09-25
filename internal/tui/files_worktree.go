package tui

import (
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/fuzzy"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// worktreeFileList is the working tree's files as F lists them: the tracked
// files (ls-files) minus those deleted in the working tree or the index,
// plus the untracked files the status already knows — no extra git walk
// (an untracked scan is the slow half of git status on a large tree).
// Sorted and deduplicated; untracked names the untracked ones.
func worktreeFileList(tracked []string, st model.WorkingTreeStatus) (paths []string, untracked map[string]bool) {
	deleted := map[string]bool{}
	untracked = map[string]bool{}
	for _, f := range st.Files {
		switch {
		case f.Kind == model.KindUntracked:
			untracked[f.Path] = true
		case f.Staged == 'D' || f.Unstaged == 'D':
			deleted[f.Path] = true
		}
	}
	for _, p := range tracked {
		if !deleted[p] {
			paths = append(paths, p)
		}
	}
	for p := range untracked {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return slices.Compact(paths), untracked
}

// worktreeFiles is the files view's working-tree mode (F): every file on
// disk, filtered by a fuzzy query kept here — NOT in filesView.query, whose
// substring filter (visible) would drop fuzzy matches. Each query change
// rebuilds filesView.lines through wtSetQuery.
type worktreeFiles struct {
	all       []string
	untracked map[string]bool
	query     string
	typing    bool
	loading   bool
}

// inWorktreeFiles reports whether the files view shows the working tree (F).
func (m Model) inWorktreeFiles() bool {
	return m.filesView != nil && m.filesMode == filesModeWorktree && m.wtFiles != nil
}

// openWorktreeFiles opens F's window: the files view in working-tree mode,
// over the whole left column, loading the tracked files off-thread.
func (m Model) openWorktreeFiles() (Model, tea.Cmd) {
	if !m.opsIdle() || m.inWorktreeFiles() {
		return m, nil
	}
	m = m.beginFilesView()
	m.filesMode = filesModeWorktree
	m.wtFiles = &worktreeFiles{loading: true}
	m.filesView = &contentPopup{lines: []contentLine{{text: i18n.T("(loading…)")}}}
	m.filesTreeFocused = true
	return m, m.loadLsFilesCmd()
}

// wtLoaded fills the window from ls-files and the status's untracked files.
func (m Model) wtLoaded(msg lsFilesMsg) (Model, tea.Cmd) {
	w := m.wtFiles
	if msg.err != nil {
		m.statusMsg = i18n.T("file finder: %s", msg.err.Error())
		ret, parked := m.filesReturnFocus, m.filesReturnLayers
		m = m.closeFilesView()
		m.focus = ret
		return m.restoreParkedLayers(parked), nil
	}
	w.all, w.untracked = worktreeFileList(msg.paths, m.status)
	w.loading = false
	m.wtSetQuery(w.query)
	return m.wtCursorMoved()
}

// wtSetQuery is the one chokepoint for the filter: it sets the query and
// rebuilds the rows — every file with no query, else the fuzzy-ranked best
// fileFinderLimit — with the cursor back on the first.
func (m Model) wtSetQuery(q string) {
	w, p := m.wtFiles, m.filesView
	w.query = q
	paths := w.all
	if q != "" {
		paths = nil
		for _, r := range fuzzy.Rank(q, w.all, fileFinderLimit) {
			paths = append(paths, r.S)
		}
	}
	lines := make([]contentLine, 0, len(paths))
	for _, path := range paths {
		text := path
		if w.untracked[path] {
			text += "  " + i18n.T("(untracked)")
		}
		lines = append(lines, contentLine{text: text, path: path})
	}
	if len(lines) == 0 {
		lines = []contentLine{{text: i18n.T("(no files)")}}
		if q != "" {
			lines = nil // the render says (no match)
		}
	}
	p.lines, p.sel = lines, 0
}

// wtSelected is the path under the cursor ("" on a placeholder).
func (m Model) wtSelected() string {
	vis := m.filesView.visible()
	if s := m.filesView.sel; s >= 0 && s < len(vis) {
		return vis[s].path
	}
	return ""
}

// wtTitle is the window's title: what it shows and how many.
func (m Model) wtTitle() string {
	w := m.wtFiles
	if w.loading {
		return i18n.T("Files (working tree)  (loading…)")
	}
	n := len(w.all)
	if w.query != "" {
		n = len(m.filesView.visible())
	}
	return i18n.T("Files (working tree)  %d/%d", n, len(w.all))
}

// wtSearchLine is the filter line under the title ("" = none).
func (m Model) wtSearchLine() string {
	switch w := m.wtFiles; {
	case w.typing:
		return "/" + w.query + "█"
	case w.query != "":
		return "/" + w.query
	}
	return ""
}

// wtMove moves the cursor by delta; the preview follows once it settles.
func (m Model) wtMove(delta int) (Model, tea.Cmd) {
	m.filesView.move(delta)
	return m.wtCursorMoved()
}

// updateWorktreeFilesKey routes a key in F's window. The preview's own
// selection and search hooks ran first (updateFilesViewKey); the commit-list
// side's keys do not apply here — there is no commit list.
func (m Model) updateWorktreeFilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	w, p := m.wtFiles, m.filesView
	if w.typing {
		if nm, nq, handled, commit := m.recallUpdate(scopeFiletree, msg, w.query); handled {
			m = nm
			m.wtSetQuery(nq)
			var cmd tea.Cmd
			m, cmd = m.wtCursorMoved()
			if commit {
				w.typing = false
				var rec tea.Cmd
				m, rec = m.recordSearch(scopeFiletree, w.query)
				return m, tea.Batch(cmd, rec)
			}
			return m, cmd
		} else {
			m = nm
		}
		if filterMotion(msg, p.move, m.filesPageRows()) {
			return m.wtCursorMoved()
		}
		switch msg.Type {
		case tea.KeyEsc:
			w.typing = false
			m.wtSetQuery("")
		case tea.KeyEnter:
			w.typing = false
			return m.recordSearch(scopeFiletree, w.query)
		case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete:
			if r := []rune(w.query); len(r) > 0 {
				m.wtSetQuery(string(r[:len(r)-1]))
			}
		case tea.KeySpace:
			m.wtSetQuery(w.query + " ")
		case tea.KeyRunes:
			m.wtSetQuery(w.query + string(msg.Runes))
		default:
			return m, nil
		}
		return m.wtCursorMoved()
	}
	if !m.filesTreeFocused { // the preview holds the keys: scroll it, or come back
		return m.updateWorktreePreviewKey(msg)
	}
	switch msg.String() {
	case "/":
		w.typing = true
		m = m.recallReset()
	case "esc":
		if w.query != "" {
			m.wtSetQuery("")
			return m.wtCursorMoved()
		}
		ret, parked := m.filesReturnFocus, m.filesReturnLayers
		m = m.closeFilesView()
		m.focus = ret
		return m.restoreParkedLayers(parked), nil
	case "up", "k":
		return m.wtMove(-1)
	case "down", "j":
		return m.wtMove(1)
	case "pgup":
		return m.wtMove(-m.filesPageRows())
	case "pgdown":
		return m.wtMove(m.filesPageRows())
	case "home":
		return m.wtMove(-len(p.lines))
	case "end":
		return m.wtMove(len(p.lines))
	case "enter", ".":
		return m.wtActionMenu(), nil
	case "right", "tab":
		if m.filesPreview != nil {
			m.filesTreeFocused = false
		}
	case "ctrl+w":
		p.mode = p.mode.next()
		p.hscroll = 0
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
	case "g":
		return m.openBookmarkSwitcher()
	case "G":
		return m.openShelfSwitcher()
	}
	return m, nil
}

// updateWorktreePreviewKey is the focused live preview: the pager keys scroll
// it; left, tab and esc hand the keyboard back to the list.
func (m Model) updateWorktreePreviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.filesPreview
	if d == nil {
		m.filesTreeFocused = true
		return m, nil
	}
	p := d.p
	rows := m.filePreviewRowsCap()
	scroll := func(delta int) { p.sel = previewClamp(p.sel+delta, len(p.lines), rows, p.mode) }
	switch msg.String() {
	case "left", "tab", "shift+tab", "esc":
		m.filesTreeFocused = true
	case "alt+up":
		m.movePreviewCursor(-1)
	case "alt+down":
		m.movePreviewCursor(1)
	case "up", "k":
		scroll(-1)
	case "down", "j":
		scroll(1)
	case "pgup":
		scroll(-rows)
	case "pgdown", " ":
		scroll(rows)
	case "home":
		p.sel = 0
	case "end":
		p.sel = previewClamp(len(p.lines), len(p.lines), rows, p.mode)
	case "ctrl+w":
		p.mode = p.mode.next()
		p.hscroll = 0
	}
	return m, nil
}

// wtPreviewSettle is how long the cursor must rest before the live preview
// reads the file: a held arrow over a slow mount reads only where it stops.
const wtPreviewSettle = 150 * time.Millisecond

// wtPreviewMsg is the settle tick for the row at path; gen drops every tick
// a later cursor move superseded.
type wtPreviewMsg struct {
	gen  int
	path string
}

// wtCursorMoved schedules the live preview for the row now under the
// cursor (none on a placeholder or an empty match).
func (m Model) wtCursorMoved() (Model, tea.Cmd) {
	m.wtPreviewGen++
	path := m.wtSelected()
	if path == "" {
		m.filesPreview = nil
		m.filesTreeFocused = true
		return m, nil
	}
	gen := m.wtPreviewGen
	return m, tea.Tick(wtPreviewSettle, func(time.Time) tea.Msg { return wtPreviewMsg{gen: gen, path: path} })
}

// wtPreviewSettled shows the settled row's working-tree file in the right
// column. The document is NOT an open file: scrolling past files must not
// fill the ctrl+\ list (View file content opens one). The list keeps the
// keys; → moves them to the preview.
func (m Model) wtPreviewSettled(msg wtPreviewMsg) (Model, tea.Cmd) {
	if !m.inWorktreeFiles() || msg.gen != m.wtPreviewGen || m.wtSelected() != msg.path {
		return m, nil
	}
	if d := m.filesPreview; d != nil && d.path == msg.path {
		return m, nil
	}
	d := newOpenFile(fileSource{kind: srcWorktree}, msg.path)
	m.filesPreview = d
	return m, m.loadDoc(d)
}
