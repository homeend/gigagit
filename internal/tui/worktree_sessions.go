package tui

import (
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

type wtEntry struct {
	wt   int
	sess domain.SessionID
}

// worktreeEntries is the Worktrees list in display order: each worktree
// followed by its agent sessions (this repo's only — other repos' sessions
// live in the ctrl+\ popup).
func (m Model) worktreeEntries() []wtEntry {
	byDir := map[string][]domain.SessionInfo{}
	for _, info := range domain.Sessions().List() {
		byDir[filepath.Clean(info.Dir)] = append(byDir[filepath.Clean(info.Dir)], info)
	}
	out := make([]wtEntry, 0, len(m.worktrees))
	for i, w := range m.worktrees {
		out = append(out, wtEntry{wt: i})
		for _, info := range byDir[filepath.Clean(w.Path)] {
			out = append(out, wtEntry{wt: i, sess: info.ID})
		}
	}
	return out
}

func sessionRowText(info domain.SessionInfo) string {
	if info.State == domain.SessionExited {
		return "  └ ○ " + info.Label + "  " + i18n.T("exited (%d)", info.ExitCode)
	}
	return "  └ ● " + info.Label + "  " + i18n.T("running %s", formatElapsed(time.Since(info.Started)))
}

// selectedSession resolves a session sub-row under the Worktrees cursor.
func (m Model) selectedSession() (domain.SessionInfo, bool) {
	e, ok := m.selectedWorktreeEntry()
	if !ok || e.sess == "" {
		return domain.SessionInfo{}, false
	}
	s, ok := domain.Sessions().Get(e.sess)
	if !ok {
		return domain.SessionInfo{}, false
	}
	return s.Info(), true
}

func (m Model) selectedWorktreeEntry() (wtEntry, bool) {
	idx := m.displayIndices(panelWorktrees)
	sel := m.sel[panelWorktrees]
	if sel < 0 || sel >= len(idx) {
		return wtEntry{}, false
	}
	ents := m.worktreeEntries()
	if idx[sel] >= len(ents) {
		return wtEntry{}, false
	}
	return ents[idx[sel]], true
}
