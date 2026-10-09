package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/fuzzy"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// worktreeFiles is the files view's working-tree mode (F): every file on
// disk, filtered by a fuzzy query kept here — NOT in filesView.query, whose
// substring filter (visible) would drop fuzzy matches. Each query change
// rebuilds filesView.lines through wtSetQuery.
type worktreeFiles struct {
	all       []string
	untracked map[string]bool
	letters   map[string]string // path → its status letter ("" = clean)
	query     string            // the filter in force (field.Value() while typing)
	field     textfield         // the editor behind query while typing: cursor-aware
	typing    bool
	loading   bool
	keepPath  string // the path under the cursor when the worktree went to sleep (sleepFWindow): the re-read lands the cursor on it
	gen       int    // the list read the window waits for: sleepFWindow bumps it, so a read in flight at sleep (queued, replayed) cannot beat the return's fresh one
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

// openWorktreeFilesHere opens F's window from wherever the keyboard is: over
// a window on the stack (a diff, history, blame), that window is parked and
// esc brings it back (handOffToFilesView).
func (m Model) openWorktreeFilesHere() (Model, tea.Cmd) {
	if m.topLayer() != nil {
		return m.handOffToFilesView(Model.openWorktreeFiles)
	}
	return m.openWorktreeFiles()
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
	w.all, w.untracked = domain.WorktreeFileList(msg.paths, m.status)
	w.letters = statusLetters(m.status)
	w.loading = false
	m.wtSetQuery(w.query)
	return m.wtCursorMoved()
}

// wtSetQuery is the one chokepoint for the filter: it sets the query and
// rebuilds the rows — every file with no query, else the fuzzy-ranked best
// fileFinderLimit — as the commit view's tree (commitFileLines: root files,
// then one heading per directory), with the cursor on the best-ranked file
// (the first with no query), never on a heading.
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
	if len(paths) == 0 {
		p.lines, p.sel = nil, 0 // the render says (no match)
		if q == "" {
			p.lines = []contentLine{{text: i18n.T("(no files)")}}
		}
		return
	}
	files := make([]model.CommitFile, 0, len(paths))
	for _, path := range paths {
		files = append(files, model.CommitFile{Path: path, Status: w.letter(path)})
	}
	p.lines, p.sel = commitFileLines(files), 0
	want := ""            // no query: the tree's first file row
	if w.keepPath != "" { // back from sleep: the path the cursor was on, if the list still has it
		for _, path := range paths {
			if path == w.keepPath {
				want = path
				break
			}
		}
		w.keepPath = ""
	}
	if q != "" && want == "" {
		want = paths[0] // the best-ranked match
	}
	for i, l := range p.lines {
		if l.path != "" && (want == "" || l.path == want) {
			p.sel = i
			break
		}
	}
}

// letter is a file row's status column: "?" for an untracked file, else its
// status letter; a clean file's is blank so the names stay in one column.
func (w *worktreeFiles) letter(path string) string {
	if w.untracked[path] {
		return "?"
	}
	if l := w.letters[path]; l != "" {
		return l
	}
	return " "
}

// statusLetters maps each changed path to the one letter F's rows show: the
// unstaged letter when there is one, else the staged one.
func statusLetters(st model.WorkingTreeStatus) map[string]string {
	out := make(map[string]string, len(st.Files))
	for _, f := range st.Files {
		switch {
		case f.Kind == model.KindUntracked:
			continue
		case f.Unstaged != '.' && f.Unstaged != 0 && f.Unstaged != ' ':
			out[f.Path] = string(f.Unstaged)
		case f.Staged != '.' && f.Staged != 0 && f.Staged != ' ':
			out[f.Path] = string(f.Staged)
		}
	}
	return out
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
		n = 0
		for _, l := range m.filesView.visible() {
			if l.path != "" {
				n++
			}
		}
	}
	return i18n.T("Files (working tree)  %d/%d", n, len(w.all))
}

// wtSearchLine is the filter line under the title ("" = none).
func (m Model) wtSearchLine() string {
	switch w := m.wtFiles; {
	case w.typing:
		return "/" + w.field.View(true)
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
			w.field = newTextField(nq)
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
		default:
			// The field edits at its cursor: runes, space, backspace/delete,
			// ←/→ (ctrl or alt: by word), home/end, ctrl+w. Anything else
			// is swallowed while typing.
			if !w.field.HandleEditKey(msg) {
				return m, nil
			}
			if v := w.field.Value(); v != w.query {
				m.wtSetQuery(v)
			}
		}
		return m.wtCursorMoved()
	}
	if !m.filesTreeFocused { // the preview holds the keys: scroll it, or come back
		return m.updateWorktreePreviewKey(msg)
	}
	switch msg.String() {
	case "/":
		w.typing = true
		w.field = newTextField(w.query) // edit the kept query, cursor at its end
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
	case "ctrl+t": // the list over the whole body, and back
		m.filesFull = !m.filesFull
	case "ctrl+]": // the file under the cursor, open in the background
		return m.wtBackgroundRow()
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
	scroll := func(delta int) { p.scrollBy(delta, rows) }
	switch msg.String() {
	case "left", "tab", "shift+tab", "esc":
		m = m.focusTree()
	case "ctrl+t":
		m.previewFull = !m.previewFull
	case "ctrl+]":
		m = m.wtBackgroundPreview()
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
		p.sel = p.clampTop(len(p.lines), rows)
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
	slotStamp // the slot it was asked from (slot_msg.go)
	gen       int
	path      string
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
	slot := m.stamp()
	return m, m.tick(wtPreviewSettle, func(time.Time) tea.Msg { return wtPreviewMsg{slotStamp: slot, gen: gen, path: path} })
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
	d := m.newOpenFile(fileSource{kind: srcWorktree}, msg.path)
	m.filesPreview = d
	return m, m.loadDoc(d)
}

// previewMaximized reports whether a focused file preview fills the body
// (ctrl+t on it). Handing the keys back to the list ends it (focusTree).
func (m Model) previewMaximized() bool {
	return m.previewFull && m.filesView != nil && m.filesPreview != nil && !m.filesTreeFocused
}

// wtBackgroundPreview is ctrl+] on F's live preview: the file joins the open
// files (ctrl+\) in the background — the one already open, if it is — and
// the keys go back to the list, where the preview goes on following the
// cursor. A file dropped over the cap is what the status names instead.
func (m Model) wtBackgroundPreview() Model {
	d := m.filesPreview
	if open := m.openFiles.find(m.currentWorktree, d.key()); open != nil {
		d = open
	}
	d.backgrounded = true
	m.statusMsg = ""
	m = m.registerDoc(d)
	if m.statusMsg == "" {
		m.statusMsg = i18n.T("%s is in the background — ctrl+\\ lists open files", d.path)
	}
	return m.focusTree()
}

// wtBackgroundRow is ctrl+] on a row of F's list: that file joins the open
// files (ctrl+\) in the background, loaded off-thread. The preview's own
// document is used when it shows the file, one already open is reused; the
// keys stay on the list.
func (m Model) wtBackgroundRow() (Model, tea.Cmd) {
	path := m.wtSelected()
	if path == "" {
		return m, nil
	}
	if d := m.filesPreview; d != nil && d.path == path {
		return m.wtBackgroundPreview(), nil
	}
	src := fileSource{kind: srcWorktree}
	d := m.openFiles.find(m.currentWorktree, docKey(src, path))
	var load tea.Cmd
	if d == nil {
		d = m.newOpenFile(src, path)
		load = m.loadDoc(d)
	}
	d.backgrounded = true
	m.statusMsg = ""
	m = m.registerDoc(d)
	if m.statusMsg == "" {
		m.statusMsg = i18n.T("%s is in the background — ctrl+\\ lists open files", d.path)
	}
	return m, load
}
