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
	case "S", "esc":
		m.stashView = nil
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
		m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
		m.filesTitle = "Files " + e.Ref + " " + e.Subject
		m.filesHash = "" // set when the SHA resolves
		m.filesTreeFocused = true
		m.filesStashTag = e.Ref
		return m, m.loadStashFilesCmd(e.Ref)
	case "enter":
		// stash-action popup — implemented in Chunk C; no-op until then.
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
	return m.renderListBox("Stashes", rows, v.sel, boxW, boxH, m.filesView == nil)
}

// openStashView opens (or refreshes) the stash list window.
func (m Model) openStashView() (Model, tea.Cmd) {
	m.stashView = &stashView{loading: true, tag: "stash"}
	return m, m.loadStashListCmd(m.stashView.tag)
}
