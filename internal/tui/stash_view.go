package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// stashView is the stash list, rendered in the right column (over Commits).
type stashView struct {
	entries []model.StashEntry
	sel     int      // cursor: an index into visible(), NOT entries
	query   string   // `/` filter: case-insensitive substring of "ref  subject"; "" = all
	typing  bool     // `/` input mode is capturing keys (mirrors Model.filterTyping)
	mode    dispMode // text display mode; z cycles
	hscroll int      // modeScroll horizontal offset
	loading bool
	err     error
	tag     string // gates stale loads
}

// stashRowText is the text a stash row shows and the `/` filter matches on.
func stashRowText(e model.StashEntry) string {
	return e.Ref + "  " + e.Subject
}

// visible returns the entries indices that pass the `/` filter, in list order.
// It is the single source of truth for what the list shows: the cursor, the
// action menu, the file tree follow-live, and rendering all consume it.
func (v *stashView) visible() []int {
	idx := make([]int, 0, len(v.entries))
	q := strings.ToLower(v.query)
	for i, e := range v.entries {
		if q != "" && !strings.Contains(strings.ToLower(stashRowText(e)), q) {
			continue
		}
		idx = append(idx, i)
	}
	return idx
}

// current returns the entry under the cursor (ok=false on an empty list or
// when the filter hides every entry).
func (v *stashView) current() (model.StashEntry, bool) {
	vis := v.visible()
	if v.sel < 0 || v.sel >= len(vis) {
		return model.StashEntry{}, false
	}
	return v.entries[vis[v.sel]], true
}

// clampSel pins the cursor into the visible range (after a reload or a filter
// change that shrank the list).
func (v *stashView) clampSel() {
	n := len(v.visible())
	if v.sel > n-1 {
		v.sel = n - 1
	}
	if v.sel < 0 {
		v.sel = 0
	}
}

// setQuery re-filters and keeps the cursor on the same stash when it is still
// visible, else on the nearest visible entry at/after it — the Commits panel's
// `/` snapping rule, so typing never jumps back to the top.
func (v *stashView) setQuery(q string) {
	anchor := -1
	if vis := v.visible(); v.sel >= 0 && v.sel < len(vis) {
		anchor = vis[v.sel]
	}
	v.query = q
	vis := v.visible()
	v.sel = 0
	for i, ei := range vis {
		v.sel = i
		if ei >= anchor {
			break
		}
	}
}

type stashListMsg struct {
	tag     string
	entries []model.StashEntry
	err     error
}

func (m Model) loadStashListCmd(tag string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		es, err := svc.StashList(context.Background())
		return stashListMsg{tag: tag, entries: es, err: err}
	}
}

type stashFilesMsg struct {
	tag   string // the stash ref
	sha   string
	lines []contentLine
	err   error
}

// loadStashFilesCmd resolves the stash ref to a SHA, then loads its changed
// files, tagged by ref for stale-gating. A -u stash stores its untracked
// files in a THIRD parent (^3, a root commit) invisible to the first-parent
// diff — resolve it best-effort and merge its files in with a per-line sha,
// so diff/preview/history read them from the right commit. No ^3 = a plain
// stash (skip); a failed ^3 file read degrades to the tracked-only list.
func (m Model) loadStashFilesCmd(ref string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		sha, err := svc.StashCommit(context.Background(), ref)
		if err != nil {
			return stashFilesMsg{tag: ref, err: err}
		}
		files, err := svc.CommitFiles(context.Background(), sha)
		if err != nil {
			return stashFilesMsg{tag: ref, sha: sha, err: err}
		}
		lines := commitFileLines(files)
		if usha, uerr := svc.StashCommit(context.Background(), ref+"^3"); uerr == nil {
			if ufiles, ferr := svc.CommitFiles(context.Background(), usha); ferr == nil && len(ufiles) > 0 {
				upaths := make(map[string]bool, len(ufiles))
				for _, f := range ufiles {
					upaths[f.Path] = true
				}
				lines = commitFileLines(append(files, ufiles...))
				for i := range lines {
					if !lines[i].heading && upaths[lines[i].path] {
						lines[i].sha = usha
					}
				}
			}
		}
		return stashFilesMsg{tag: ref, sha: sha, lines: lines}
	}
}

// updateStashViewKey routes one key while the stash list owns focus (i.e. no
// file tree is open over it). It swallows non-handled keys.
func (m Model) updateStashViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.stashView
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if v.typing {
		// `/` input mode, the Commits-panel contract: every rune narrows the
		// list live, ↑/↓ still move, enter keeps the filter, esc drops it.
		// Everything else is swallowed so no list key fires mid-query.
		switch msg.Type {
		case tea.KeyEsc:
			v.typing = false
			v.setQuery("")
		case tea.KeyEnter:
			v.typing = false
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(v.query); len(r) > 0 {
				v.setQuery(string(r[:len(r)-1]))
			}
		case tea.KeySpace:
			v.setQuery(v.query + " ")
		case tea.KeyRunes:
			v.setQuery(v.query + string(msg.Runes))
		case tea.KeyUp:
			if v.sel > 0 {
				v.sel--
			}
		case tea.KeyDown:
			if v.sel < len(v.visible())-1 {
				v.sel++
			}
		}
		return m, nil
	}
	switch msg.String() {
	case ".":
		return m.openActionMenu(), nil
	case "/":
		// Start a fresh filter. The cursor stays put (an empty query shows
		// the full list); each typed rune snaps to the nearest match at/after it.
		v.setQuery("")
		v.typing = true
		return m, nil
	case "ctrl+r": // clear a kept filter, cursor on the same stash
		v.setQuery("")
		return m, nil
	case "g": // global bookmark quick-switcher
		return m.openBookmarkSwitcher()
	case "G": // global shelf quick-switcher
		return m.openShelfSwitcher()
	case "F": // global fuzzy file finder
		return m.openFileFinder()
	case "ctrl+w":
		v.mode = v.mode.next()
		v.hscroll = 0
		return m, nil
	case "shift+left":
		if v.mode == modeScroll && v.hscroll > 0 {
			if v.hscroll -= m.hscrollStep(); v.hscroll < 0 {
				v.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if v.mode == modeScroll {
			v.hscroll += m.hscrollStep()
		}
		return m, nil
	case "S", "esc":
		return m.closeStashView(), nil
	case "tab", "shift+tab":
		// Cycle focus exactly like the main dispatch: the stash list occupies
		// the Commits slot in the focus order, so tab walks into the left
		// column (and the normal handler cycles back here). The window stays
		// open, dimmed, like the ← release.
		dir := 1
		if msg.String() == "shift+tab" {
			dir = -1
		}
		m = m.rememberLeftFocus()
		m.focus = nextInOrder(m.focusOrder(), m.focus, dir)
		return m, nil
	case "left":
		// Release focus to the left column (the stash list stays open, dimmed),
		// so the user can inspect the Status/Branches/Worktrees panels — e.g.
		// the files an applied/popped stash just changed. → returns here.
		if m.width <= 0 || m.width >= 40 {
			m.focus = m.lastLeftPanel
		}
		return m, nil
	case "down", "j":
		if v.sel < len(v.visible())-1 {
			v.sel++
		}
		return m, nil
	case "up", "k":
		if v.sel > 0 {
			v.sel--
		}
		return m, nil
	case "pgdown":
		v.sel += m.pageStep()
		v.clampSel()
		return m, nil
	case "pgup":
		if v.sel -= m.pageStep(); v.sel < 0 {
			v.sel = 0
		}
		return m, nil
	case "l":
		e, ok := v.current()
		if !ok {
			return m, nil
		}
		if m.width > 0 && m.width < 40 {
			m.statusMsg = i18n.T("terminal too narrow for the files view")
			return m, nil
		}
		// Opens with focus on the stash list (follow-live), exactly like the commit
		// files view; ←/→ move focus to/from the tree.
		return m.openStashFiles(e.Ref, e.Subject)
	case "enter":
		// Drill into the stash's file list with focus on the tree — the
		// commits-panel enter gesture (l keeps opening on the list side; the
		// Apply/Pop/Drop menu moved to "." and stays on enter under the tree).
		e, ok := v.current()
		if !ok {
			return m, nil
		}
		if m.width > 0 && m.width < 40 {
			m.statusMsg = i18n.T("terminal too narrow for the files view")
			return m, nil
		}
		mm, cmd := m.openStashFiles(e.Ref, e.Subject)
		return mm.focusTree(), cmd
	}
	return m, nil
}

// renderStashList renders the stash entries as a bordered right-column box of
// boxW×boxH, mirroring renderPanel's framing. It is focused (owns highlight)
// while no file tree is open over it.
func (m Model) renderStashList(boxW, boxH int) string {
	v := m.stashView
	vis := v.visible()
	rows := make([]string, len(vis))
	for i, ei := range vis {
		rows[i] = stashRowText(v.entries[ei])
	}
	switch {
	case v.loading:
		rows = []string{i18n.T("(loading…)")}
	case v.err != nil:
		rows = []string{i18n.T("error: %s", v.err.Error())}
	case len(v.entries) == 0:
		rows = []string{i18n.T("(no stashes)")}
	case len(vis) == 0:
		rows = []string{i18n.T("(no match)")}
	}
	// The title carries the `/` query exactly like panelLabel does for the
	// panels: a block cursor while typing, the bare query once kept.
	label := i18n.T("Stashes")
	if v.typing {
		label += " /" + v.query + "█"
	} else if v.query != "" {
		label += " /" + v.query
	}
	// Focused (bright border, highlighted cursor) only when it owns focus:
	// m.focus is the right column AND the file tree isn't the active side.
	// Mirrors panelFocused(panelCommits) for the commit files view.
	focused := m.focus == panelCommits && !(m.filesView != nil && m.filesTreeFocused)
	return m.renderListBox(label, rows, v.sel, boxW, boxH, focused, v.mode, v.hscroll)
}

// stashFilterTyping reports whether the stash list's `/` input mode owns the
// keyboard: the list is the focused right column with no file tree over it.
func (m Model) stashFilterTyping() bool {
	return m.stashView != nil && m.stashView.typing && m.focus == panelCommits && m.filesView == nil
}

// openStashView opens the stash list window in the right column and moves focus
// there (mirroring the Commits panel): the prior left panel is remembered and
// dims via panelFocused while m.focus is panelCommits.
func (m Model) openStashView() (Model, tea.Cmd) {
	m = m.rememberLeftFocus()
	m.focus = panelCommits
	m.stashView = &stashView{loading: true, tag: "stash"}
	return m, m.loadStashListCmd(m.stashView.tag)
}

// closeStashView closes the window and restores focus to the left panel that
// had it before S was pressed. reconcileFullscreenFocus then re-asserts the
// fullscreen invariant: if closing this was the last suspending surface and
// a T pin resumes, lastLeftPanel may point at a panel the resuming pin hides
// — e.g. a Commits pin (lastLeftPanel is stale: rememberLeftFocus never
// records Commits) or a left-panel pin whose lastLeftPanel drifted to a
// different left panel while the stash list was up.
func (m Model) closeStashView() Model {
	m.stashView = nil
	m.focus = m.lastLeftPanel
	return m.reconcileFullscreenFocus()
}

// moveStashUnderFilesView shifts the stash selection by delta and fires the
// follow-live reload when it lands on a different stash (the file tree is open).
// Staleness is keyed on the ref (filesStashTag), since ref→SHA is async.
func (m Model) moveStashUnderFilesView(delta int) (tea.Model, tea.Cmd) {
	v := m.stashView
	vis := v.visible()
	s := v.sel + delta
	if s > len(vis)-1 {
		s = len(vis) - 1
	}
	if s < 0 {
		s = 0
	}
	if s == v.sel || len(vis) == 0 {
		return m, nil
	}
	v.sel = s
	e := v.entries[vis[s]]
	if e.Ref == m.filesStashTag { // the tree already shows this stash
		return m, nil
	}
	m.filesTitle = i18n.T("Files %s %s", e.Ref, e.Subject)
	m.filesContext = e.Ref + " " + e.Subject
	m.filesStashTag = e.Ref
	return m, m.loadStashFilesCmd(e.Ref)
}
