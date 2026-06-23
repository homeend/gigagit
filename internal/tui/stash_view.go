package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// stashView is the stash list, rendered in the right column (over Commits).
type stashView struct {
	entries []model.StashEntry
	sel     int
	mode    dispMode // text display mode; z cycles
	hscroll int      // modeScroll horizontal offset
	loading bool
	err     error
	tag     string // gates stale loads
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
// files, tagged by ref for stale-gating.
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
		return stashFilesMsg{tag: ref, sha: sha, lines: commitFileLines(files)}
	}
}

// updateStashViewKey routes one key while the stash list owns focus (i.e. no
// file tree is open over it). It swallows non-handled keys.
func (m Model) updateStashViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.stashView
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case ".":
		return m.openActionMenu(), nil
	case "g": // global bookmark quick-switcher
		return m.openBookmarkSwitcher()
	case "G": // global shelf quick-switcher
		return m.openShelfSwitcher()
	case "F": // global fuzzy file finder
		return m.openFileFinder()
	case "z":
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
	case "left":
		// Release focus to the left column (the stash list stays open, dimmed),
		// so the user can inspect the Status/Branches/Worktrees panels — e.g.
		// the files an applied/popped stash just changed. → returns here.
		if m.width <= 0 || m.width >= 40 {
			m.focus = m.lastLeftPanel
		}
		return m, nil
	case "down", "j":
		if v.sel < len(v.entries)-1 {
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
		if v.sel > len(v.entries)-1 {
			v.sel = len(v.entries) - 1
		}
		return m, nil
	case "pgup":
		if v.sel -= m.pageStep(); v.sel < 0 {
			v.sel = 0
		}
		return m, nil
	case "l":
		if v.sel < 0 || v.sel >= len(v.entries) {
			return m, nil
		}
		if m.width > 0 && m.width < 40 {
			m.statusMsg = "terminal too narrow for the files view"
			return m, nil
		}
		e := v.entries[v.sel]
		// Opens with focus on the stash list (follow-live), exactly like the commit
		// files view; ←/→ move focus to/from the tree.
		return m.openStashFiles(e.Ref, e.Subject)
	case "enter":
		if v.sel < 0 || v.sel >= len(v.entries) {
			return m, nil
		}
		e := v.entries[v.sel]
		m = m.pushLayer(&stashActionPopup{ref: e.Ref, subject: e.Subject})
		return m, nil
	}
	return m, nil
}

// renderStashList renders the stash entries as a bordered right-column box of
// boxW×boxH, mirroring renderPanel's framing. It is focused (owns highlight)
// while no file tree is open over it.
func (m Model) renderStashList(boxW, boxH int) string {
	v := m.stashView
	rows := make([]string, len(v.entries))
	for i, e := range v.entries {
		rows[i] = e.Ref + "  " + e.Subject
	}
	switch {
	case v.loading:
		rows = []string{"(loading…)"}
	case v.err != nil:
		rows = []string{"error: " + v.err.Error()}
	case len(v.entries) == 0:
		rows = []string{"(no stashes)"}
	}
	// Focused (bright border, highlighted cursor) only when it owns focus:
	// m.focus is the right column AND the file tree isn't the active side.
	// Mirrors panelFocused(panelCommits) for the commit files view.
	focused := m.focus == panelCommits && !(m.filesView != nil && m.filesTreeFocused)
	return m.renderListBox("Stashes", rows, v.sel, boxW, boxH, focused, v.mode, v.hscroll)
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
// had it before S was pressed.
func (m Model) closeStashView() Model {
	m.stashView = nil
	m.focus = m.lastLeftPanel
	return m
}

// moveStashUnderFilesView shifts the stash selection by delta and fires the
// follow-live reload when it lands on a different stash (the file tree is open).
// Staleness is keyed on the ref (filesStashTag), since ref→SHA is async.
func (m Model) moveStashUnderFilesView(delta int) (tea.Model, tea.Cmd) {
	v := m.stashView
	s := v.sel + delta
	if s > len(v.entries)-1 {
		s = len(v.entries) - 1
	}
	if s < 0 {
		s = 0
	}
	if s == v.sel {
		return m, nil
	}
	v.sel = s
	e := v.entries[s]
	if e.Ref == m.filesStashTag { // the tree already shows this stash
		return m, nil
	}
	m.filesTitle = "Files " + e.Ref + " " + e.Subject
	m.filesStashTag = e.Ref
	return m, m.loadStashFilesCmd(e.Ref)
}
