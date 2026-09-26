package tui

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

type wtEntry struct {
	wt   int
	sess domain.SessionID
	task domain.TaskID // a live headless AI task (a ◆ row)
}

// sub reports a sub-row (an agent session or a ◆ task), not a worktree.
func (e wtEntry) sub() bool { return e.sess != "" || e.task != "" }

// worktreeEntries is the Worktrees list in display order: each worktree
// followed by its agent sessions (this repo's only — other repos' sessions
// live in the ctrl+\ popup).
func (m Model) worktreeEntries() []wtEntry {
	byDir := map[string][]domain.SessionInfo{}
	for _, info := range domain.Sessions().List() {
		byDir[filepath.Clean(info.Dir)] = append(byDir[filepath.Clean(info.Dir)], info)
	}
	tasks := domain.Tasks().List()
	out := make([]wtEntry, 0, len(m.worktrees))
	for i, w := range m.worktrees {
		out = append(out, wtEntry{wt: i})
		for _, info := range byDir[filepath.Clean(w.Path)] {
			out = append(out, wtEntry{wt: i, sess: info.ID})
		}
		for _, info := range tasks {
			if info.Mode == domain.TaskHeadless && info.State.Live() && domain.SameCheckout(info.Worktree, w.Path) {
				out = append(out, wtEntry{wt: i, task: info.ID})
			}
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

// taskSubRowText is "  └ ◆ review a1b2c3d..9f8e7d6  queued": the kind,
// then the key's target.
func taskSubRowText(info domain.TaskInfo) string {
	target := info.Key
	if _, t, ok := strings.Cut(info.Key, " — "); ok {
		target = t
	}
	state := taskStateLabel(info.State)
	if info.State != domain.TaskQueued && !info.Started.IsZero() {
		state += " " + formatElapsed(time.Since(info.Started))
	}
	return "  └ ◆ " + taskKindLabel(info.Kind) + " " + target + "  " + state
}

// selectedTask resolves a ◆ row under the Worktrees cursor.
func (m Model) selectedTask() (domain.TaskInfo, bool) {
	e, ok := m.selectedWorktreeEntry()
	if !ok || e.task == "" {
		return domain.TaskInfo{}, false
	}
	return domain.Tasks().Get(e.task)
}
