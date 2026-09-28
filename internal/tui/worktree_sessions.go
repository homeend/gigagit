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

// sessionRowText is the Worktrees sub-row: indented under the worktree's
// two-column marker.
func sessionRowText(info domain.SessionInfo) string { return "  " + sessionRowBody(info) }

// sessionRowBody is a session sub-row without its indent: "└ ● label  running
// 12s" / "└ ○ label  exited (0)". The Branches tab indents it to its gutter.
func sessionRowBody(info domain.SessionInfo) string {
	if info.State == domain.SessionExited {
		return "└ ○ " + info.Label + "  " + i18n.T("exited (%d)", info.ExitCode)
	}
	return "└ ● " + info.Label + "  " + i18n.T("running %s", formatElapsed(time.Since(info.Started)))
}

// brEntry is one Branches row: a branch, or an agent session running in the
// worktree that branch is checked out in (a sub-row under it).
type brEntry struct {
	br     int
	sess   domain.SessionID
	review string // an AI review of the branch's current tip: its note id
}

// sub reports a sub-row (a session or a review), not a branch.
func (e brEntry) sub() bool { return e.sess != "" || e.review != "" }

// branchEntries is the Branches list in display order: each branch followed
// by the sessions of its worktree (worktreePathOf — the same lookup the row's
// path marker uses, so a sub-row appears exactly under a row that names a
// checkout). Sessions only: ◆ tasks stay on the Worktrees and Headless tabs.
func (m Model) branchEntries() []brEntry {
	byDir := map[string][]domain.SessionInfo{}
	for _, info := range domain.Sessions().List() {
		byDir[filepath.Clean(info.Dir)] = append(byDir[filepath.Clean(info.Dir)], info)
	}
	out := make([]brEntry, 0, len(m.branches))
	for i, b := range m.branches {
		out = append(out, brEntry{br: i})
		if len(byDir) > 0 {
			if path, ok := m.worktreePathOf(b.Name); ok {
				for _, info := range byDir[filepath.Clean(path)] {
					out = append(out, brEntry{br: i, sess: info.ID})
				}
			}
		}
		// The AI reviews of the branch's current tip (branch_reviews.go).
		for _, r := range m.branchReviewHeads(b) {
			out = append(out, brEntry{br: i, review: r.ID})
		}
	}
	return out
}

// selectedSession resolves a session sub-row under the cursor of the focused
// tab — Worktrees or Branches, which both interleave them.
func (m Model) selectedSession() (domain.SessionInfo, bool) {
	var id domain.SessionID
	switch m.focus {
	case panelWorktrees:
		e, ok := m.selectedWorktreeEntry()
		if !ok {
			return domain.SessionInfo{}, false
		}
		id = e.sess
	case panelBranches:
		e, ok := m.selectedBranchEntry()
		if !ok {
			return domain.SessionInfo{}, false
		}
		id = e.sess
	}
	if id == "" {
		return domain.SessionInfo{}, false
	}
	s, ok := domain.Sessions().Get(id)
	if !ok {
		return domain.SessionInfo{}, false
	}
	return s.Info(), true
}

func (m Model) selectedBranchEntry() (brEntry, bool) {
	idx := m.displayIndices(panelBranches)
	sel := m.sel[panelBranches]
	if sel < 0 || sel >= len(idx) {
		return brEntry{}, false
	}
	ents := m.branchEntries()
	if idx[sel] >= len(ents) {
		return brEntry{}, false
	}
	return ents[idx[sel]], true
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

// selectedTask resolves a ◆ row under the Worktrees cursor — only while that
// tab is focused: the cursor is per tab, and a ◆ row parked under it must not
// act from the Branches tab (which lists no tasks).
func (m Model) selectedTask() (domain.TaskInfo, bool) {
	if m.focus != panelWorktrees {
		return domain.TaskInfo{}, false
	}
	e, ok := m.selectedWorktreeEntry()
	if !ok || e.task == "" {
		return domain.TaskInfo{}, false
	}
	return domain.Tasks().Get(e.task)
}
