package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
)

// taskTrack is what this TUI remembers about the process-global AI tasks
// (domain.Tasks()): which results it applied, which ends it reported, and
// the per-task wiring a launch set up. Maps, so value copies share them.
type taskTrack struct {
	seen    map[domain.TaskID]int    // results applied so far
	ended   map[domain.TaskID]bool   // end already reported
	inbox   map[domain.TaskID]string // GG_INBOX handed to an interactive task
	fg      map[domain.TaskID]bool   // foreground: open its console once the session exists
	removed map[domain.TaskID]bool   // x'd in the Headless tab
	capWarn string                   // the last max_parallel warning shown
}

func newTaskTrack() *taskTrack {
	return &taskTrack{
		seen: map[domain.TaskID]int{}, ended: map[domain.TaskID]bool{},
		inbox: map[domain.TaskID]string{}, fg: map[domain.TaskID]bool{},
		removed: map[domain.TaskID]bool{},
	}
}

// ensureTaskTrack covers a Model built as a literal (tests) rather than by
// New; the tracker then lives on the returned copy.
func (m Model) ensureTaskTrack() Model {
	if m.taskTrack == nil {
		m.taskTrack = newTaskTrack()
	}
	return m
}

// tasksChangedMsg: something in Tasks() changed. waitTasksCmd is the ONE
// reader of the coalesced Changed channel; onTasksChanged re-arms it.
type tasksChangedMsg struct{}

func waitTasksCmd() tea.Cmd {
	ch := domain.Tasks().Changed()
	return func() tea.Msg {
		<-ch
		return tasksChangedMsg{}
	}
}

// onTasksChanged hands each task's new wiring, results and end to the TUI
// exactly once.
func (m Model) onTasksChanged() (Model, tea.Cmd) {
	var cmds []tea.Cmd
	m = m.ensureTaskTrack()
	tr := m.taskTrack
	for _, info := range domain.Tasks().List() {
		if info.Session != "" {
			if dir := tr.inbox[info.ID]; dir != "" {
				if _, ok := m.childInbox[info.Session]; !ok {
					m.childInbox[info.Session] = dir // steer_kept keeps it answered
				}
			}
			if tr.fg[info.ID] {
				delete(tr.fg, info.ID)
				var c tea.Cmd
				m, c = m.openConsole(info.Session)
				cmds = append(cmds, c)
			}
		}
		if info.Results > tr.seen[info.ID] {
			tr.seen[info.ID] = info.Results
			var c tea.Cmd
			m, c = m.applyTaskResult(info)
			cmds = append(cmds, c)
		}
		if !info.State.Live() && !tr.ended[info.ID] {
			tr.ended[info.ID] = true
			var c tea.Cmd
			m, c = m.taskEnded(info)
			cmds = append(cmds, c)
		}
	}
	if p := layerOf[*sessionsPopup](m); p != nil && !p.quitMode {
		p.hist = domain.Tasks().History() // read here, never per frame
	}
	if err := domain.Tasks().TakeStoreProblem(); err != nil {
		var c tea.Cmd
		m, c = m.stickyNotice(i18n.T("AI task history could not be saved (%s) — this session keeps it in memory", err.Error()))
		cmds = append(cmds, c)
	}
	return m, tea.Batch(append(cmds, waitTasksCmd())...)
}

// taskEnded reports an end that has no result of its own to show: a
// failure, or a conflict run that reported nothing (the repository state is
// its outcome, so the status reloads).
func (m Model) taskEnded(info domain.TaskInfo) (Model, tea.Cmd) {
	switch info.State {
	case domain.TaskFailed:
		return m.stickyNotice(i18n.T("%s failed — %s  (ctrl+\\ for details)", info.Key, info.Err))
	case domain.TaskDone:
		if info.Results == 0 && isConflictKind(info.Kind) && m.taskHere(info) {
			m.statusMsg = i18n.T("%s finished", info.Key)
			return m, m.loadCmd()
		}
	}
	return m, nil
}

// applyTaskResult routes a new result to its kind.
func (m Model) applyTaskResult(info domain.TaskInfo) (Model, tea.Cmd) {
	switch info.Kind {
	case exttool.CatCommitMessage:
		return m.applyCommitMessage(info)
	case exttool.CatReview:
		return m.applyReviewResult(info)
	case exttool.CatConflict, exttool.CatConflictComplete:
		return m.applyConflictResult(info)
	}
	return m.stickyNotice(i18n.T("%s ready — ctrl+\\", info.Key))
}

// taskHere: the task ran in the checkout the TUI shows.
func (m Model) taskHere(info domain.TaskInfo) bool {
	return domain.SameCheckout(info.Worktree, m.currentWorktree)
}

func isConflictKind(k exttool.Category) bool {
	return k == exttool.CatConflict || k == exttool.CatConflictComplete
}

// applyTasksConfig sets the cap from [tasks] max_parallel; an out-of-range
// value is clamped and warned about once per distinct value.
func (m Model) applyTasksConfig() (Model, tea.Cmd) {
	m = m.ensureTaskTrack()
	n, warn := m.cfg.Tasks.Parallel()
	domain.Tasks().SetMaxParallel(n)
	if warn == "" || warn == m.taskTrack.capWarn {
		return m, nil
	}
	m.taskTrack.capWarn = warn
	return m.stickyNotice(i18n.T("[tasks] max_parallel = %d is out of range; using %d", m.cfg.Tasks.MaxParallel, n))
}

// taskKindLabel names a kind for rows and titles.
func taskKindLabel(k exttool.Category) string {
	switch k {
	case exttool.CatCommitMessage:
		return i18n.T("commit message")
	case exttool.CatReview:
		return i18n.T("review")
	case exttool.CatConflict:
		return i18n.T("resolve conflict")
	case exttool.CatConflictComplete:
		return i18n.T("resolve & complete")
	}
	return string(k)
}

// applyConflictResult shows a conflict agent's overview (when nothing owns
// the screen) and reloads the status: the outcome is the repository state.
func (m Model) applyConflictResult(info domain.TaskInfo) (Model, tea.Cmd) {
	if !m.canShowResult(info) {
		m, c := m.stickyNotice(i18n.T("%s ready — ctrl+\\", info.Key))
		if m.taskHere(info) {
			return m, tea.Batch(c, m.loadCmd())
		}
		return m, c
	}
	m, open := m.openResultViewer(info.ID, ".md", i18n.T("Resolution overview — %s", info.Agent), info.Result, nil)
	return m, tea.Batch(open, m.loadCmd())
}
