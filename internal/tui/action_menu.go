package tui

import (
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// actionRow is one runnable action in the . menu: its stable id, the key that
// runs it, and the footer-style label. Copy rows instead carry copyText (the
// value placed on the clipboard, resolved at menu-build time) and a run handler
// invoked directly rather than by replaying key.
type actionRow struct {
	id       string
	key      string
	label    string
	copyText string
	run      func(Model) (tea.Model, tea.Cmd)
}

// availableActions returns the currently-available CONTEXT actions as menu
// rows: row-scoped first, then window-scoped, registry order within each group.
// Global (whole-app) actions are excluded — they live in the footer tail and
// have their own hotkeys. Navigation (id == "") is skipped. The dynamic copy
// rows (contextCopyRows) lead the row group.
func availableActions(m Model) []actionRow {
	// Inside a navigable content window the panel bindings don't apply (and the
	// still-true commit-files [l] binding would, if listed, replay l and close
	// the very window the menu was opened from). Offer only that window's copy
	// actions.
	if m.inContentWindow() {
		rows := m.contextCopyRows()
		if r, ok := m.shelfAddRow(); ok {
			rows = append(rows, r)
		}
		if r, ok := m.bookmarkAddRow(); ok {
			rows = append(rows, r)
		}
		if r, ok := m.compareAgainstBookmarkRow(); ok {
			rows = append(rows, r)
		}
		if r, ok := m.compareAgainstShelfRow(); ok {
			rows = append(rows, r)
		}
		if r, ok := m.compareAgainstWorkingDirRow(); ok {
			rows = append(rows, r)
		}
		return rows
	}
	var row, window []actionRow
	for _, b := range contextBindings {
		if b.id == "" || !b.when(m) {
			continue
		}
		switch b.scope {
		case scopeRow:
			row = append(row, actionRow{id: b.id, key: b.key, label: b.label})
		case scopeWindow:
			window = append(window, actionRow{id: b.id, key: b.key, label: b.label})
		}
	}
	out := append(m.contextCopyRows(), row...)
	out = append(out, window...)
	if r, ok := m.shelfAddRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.remotePruneRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.bookmarkAddRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.compareAgainstBookmarkRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.compareAgainstShelfRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.compareAgainstWorkingDirRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.renameBranchRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.rewordRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitCreateBranchRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitCreateWorktreeRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitCherryPickRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitRevertRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitSoloRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitGotoTipRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitToggleRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitShowAllRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitViewModeRow(); ok {
		out = append(out, r)
	}
	return out
}

// inContentWindow reports whether a navigable content window owns the keyboard
// (file tree, stash list, diff, history, blame), so the . menu should offer
// only that window's copy actions. Transient stack editors (interactive-rebase
// editor, hunk picker) are NOT content windows.
func (m Model) inContentWindow() bool {
	switch m.topLayer().(type) {
	case *historyView, *blameView:
		return true
	}
	if m.diffView != nil || m.filesView != nil {
		return true
	}
	if m.stashView != nil && m.focus == panelCommits {
		return true
	}
	return false
}

// contextCopyRows returns the clipboard copy actions for whatever is in view,
// with the copied text captured now (the selection is frozen while the menu is
// open). A navigable content window takes precedence over the panel selection,
// mirroring the dispatch/render chain. Empty when nothing is copyable.
func (m Model) contextCopyRows() []actionRow {
	// Precedence mirrors the dispatch/render chain: the stack surface out-ranks
	// the diff view (open a diff, then h/b, and both are live with the stack
	// surface on top), which out-ranks the file tree.
	switch s := m.topLayer().(type) {
	case *historyView:
		if s.sel >= 0 && s.sel < len(s.commits) {
			fc := s.commits[s.sel]
			return m.fileCopyRows(fc.Path, fc.Hash)
		}
		return m.fileCopyRows(s.ctx.path, s.ctx.rev)
	case *blameView:
		return m.fileCopyRows(s.ctx.path, s.ctx.rev)
	}
	if v := m.diffView; v != nil {
		return m.fileCopyRows(v.title, v.rev) // title = path; rev = commit ("" = working tree)
	}
	if v := m.filesView; v != nil {
		var rows []actionRow
		if m.filesTreeFocused {
			if vis := v.visible(); v.sel >= 0 && v.sel < len(vis) && vis[v.sel].path != "" {
				rows = append(rows, m.fileCopyPathName(vis[v.sel].path)...)
			}
		}
		if m.filesHash != "" { // a commit's files (a stash file tree has no commit id)
			rows = append(rows, m.copyRow("copy-commit-id", "Copy commit id", "Copied commit id "+shortHash(m.filesHash), m.filesHash))
		}
		return rows
	}
	if v := m.stashView; v != nil && m.focus == panelCommits {
		if v.sel >= 0 && v.sel < len(v.entries) {
			ref := v.entries[v.sel].Ref
			return []actionRow{m.copyRow("copy-stash-ref", "Copy stash ref", "Copied stash ref "+ref, ref)}
		}
		return nil
	}
	switch {
	case m.focus == panelCommits:
		if bi, ok := m.backingIndex(panelCommits); ok {
			h := m.commits[bi].Hash
			return []actionRow{m.copyRow("copy-commit-id", "Copy commit id", "Copied commit id "+shortHash(h), h)}
		}
	case m.isFilesPanel(m.focus):
		if bi, ok := m.backingIndex(m.focus); ok {
			return m.fileCopyPathName(m.status.Files[bi].Path)
		}
	case m.focus == panelBranches:
		if bi, ok := m.backingIndex(panelBranches); ok {
			name := m.branches[bi].Name
			return []actionRow{m.copyRow("copy-branch-name", "Copy branch name", "Copied branch name "+name, name)}
		}
	case m.focus == panelRemotes:
		if bi, ok := m.backingIndex(panelRemotes); ok {
			name := m.remoteBranches[bi].Name
			return []actionRow{m.copyRow("copy-branch-name", "Copy branch name", "Copied branch name "+name, name)}
		}
	}
	return nil
}

// fileCopyRows returns the path + name copy rows for a file, plus a commit-id
// row when rev is a real commit (rev == "" means the working tree).
func (m Model) fileCopyRows(filePath, rev string) []actionRow {
	rows := m.fileCopyPathName(filePath)
	if rev != "" {
		rows = append(rows, m.copyRow("copy-commit-id", "Copy commit id", "Copied commit id "+shortHash(rev), rev))
	}
	return rows
}

// fileCopyPathName returns the path + basename copy rows for a file.
func (m Model) fileCopyPathName(p string) []actionRow {
	return []actionRow{
		m.copyRow("copy-file-path", "Copy file path", "Copied path: "+p, p),
		m.copyRow("copy-file-name", "Copy file name", "Copied file name: "+path.Base(p), path.Base(p)),
	}
}

// copyRow builds a menu-only copy action: its run handler fires the clipboard
// command carrying the pre-resolved success message and text.
func (m Model) copyRow(id, label, okMsg, text string) actionRow {
	return actionRow{
		id:       id,
		label:    label,
		copyText: text,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.copyToClipboardCmd(okMsg, text)
		},
	}
}

// synthKey reproduces the keypress that runs an action's key, for replay
// through Update. enter/space are the only non-rune keys any action id carries;
// everything else (single runes incl. / , ? .) is a KeyRunes.
func synthKey(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}

// actionMenu is the . overlay: a window-primitive list of runnable actions.
type actionMenu struct {
	rows    []actionRow
	sel     int
	typing  bool // / filter input
	query   string
	mode    dispMode
	hscroll int
}

func (a *actionMenu) visible() []actionRow {
	if a.query == "" {
		return a.rows
	}
	q := strings.ToLower(a.query)
	var out []actionRow
	for _, r := range a.rows {
		if strings.Contains(strings.ToLower(r.label), q) {
			out = append(out, r)
		}
	}
	return out
}

// move advances the selection by d, wrapping around the ends: up from the first
// row goes to the last, down from the last goes to the first.
func (a *actionMenu) move(d int) {
	n := len(a.visible())
	if n == 0 {
		a.sel = 0
		return
	}
	a.sel = ((a.sel+d)%n + n) % n // wrap, handling negative d
}

// openActionMenu builds the menu from the available actions, narrowed by the
// menu_actions allowlist when set.
func (m Model) openActionMenu() Model {
	rows := availableActions(m)
	if ids := m.cfg.UI.MenuActions; len(ids) > 0 {
		byID := make(map[string]actionRow, len(rows))
		for _, r := range rows {
			byID[r.id] = r
		}
		ordered := make([]actionRow, 0, len(ids))
		for _, id := range ids {
			if r, ok := byID[id]; ok {
				ordered = append(ordered, r)
			}
		}
		rows = ordered
	}
	m.actionMenu = &actionMenu{rows: rows}
	return m
}

// runVisibleRow closes the menu and either invokes the row's direct handler
// (copy actions) or replays its key through Update (every other action, which
// reaches the base-layout handler now that the menu is nil).
func (m Model) runVisibleRow(sel int) (tea.Model, tea.Cmd) {
	vis := m.actionMenu.visible()
	if sel < 0 || sel >= len(vis) {
		m.actionMenu = nil
		return m, nil
	}
	r := vis[sel]
	m.actionMenu = nil
	if r.run != nil {
		return r.run(m)
	}
	return m.Update(synthKey(r.key))
}

func (m Model) updateActionMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.actionMenu
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if a.typing { // / filter input captures keys
		switch msg.Type {
		case tea.KeyEsc:
			a.typing = false
			a.query = ""
			a.sel = 0
		case tea.KeyEnter:
			return m.runVisibleRow(a.sel)
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(a.query); len(r) > 0 {
				a.query = string(r[:len(r)-1])
			}
			a.sel = 0
		case tea.KeyRunes:
			a.query += string(msg.Runes)
			a.sel = 0
		}
		return m, nil
	}
	switch msg.String() {
	case "z":
		a.mode = a.mode.next()
		a.hscroll = 0
		return m, nil
	case "shift+left":
		if a.mode == modeScroll && a.hscroll > 0 {
			if a.hscroll -= m.hscrollStep(); a.hscroll < 0 {
				a.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if a.mode == modeScroll {
			a.hscroll += m.hscrollStep()
		}
		return m, nil
	case "esc", "q":
		// Close like every other popup; q must NOT fall through to the quit row.
		m.actionMenu = nil
		return m, nil
	case "/":
		a.typing = true
		a.query = ""
		a.sel = 0
		return m, nil
	case "up", "k":
		a.move(-1)
		return m, nil
	case "down", "j":
		a.move(1)
		return m, nil
	case "enter":
		return m.runVisibleRow(a.sel)
	}
	// Direct key: run the visible row whose key matches. Space reports its
	// String() as " ", so normalize it to the registry's "space".
	pressed := msg.String()
	if msg.Type == tea.KeySpace {
		pressed = "space"
	}
	vis := a.visible()
	for i, r := range vis {
		// r.key == "" marks a menu-only copy row (no replayable key); never
		// match it on an empty pressed string.
		if r.key != "" && r.key == pressed {
			return m.runVisibleRow(i)
		}
	}
	return m, nil
}

// renderActionMenu draws the overlay (composited by render via overlayCenter).
func (m Model) renderActionMenu() string {
	a := m.actionMenu
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	vis := a.visible()
	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight("  (no match)", textW)}
	} else {
		wr := make([]winRow, len(vis))
		for i, r := range vis {
			prefix := "  "
			var st lipgloss.Style
			if i == a.sel {
				prefix, st = "> ", selectedRow
			}
			wr[i] = winRow{text: prefix + r.label, style: st}
		}
		h := len(vis)
		if h > 14 {
			h = 14
		}
		bodyLines = renderWindow(wr, winOpts{w: textW, h: h, mode: a.mode, anchor: a.sel, hscroll: a.hscroll})
	}
	header := "Actions"
	if a.typing {
		header += "  /" + a.query + "█"
	} else if a.query != "" {
		header += "  /" + a.query
	}
	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "", "[key]/[enter] run  [/] filter  [z] mode  [esc] close")
	return popupBox(inner, strings.Join(parts, "\n"))
}
