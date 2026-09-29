package tui

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
)

// The ctrl+\ popup's Headless tab (spec ruling 3): every AI task, live ones
// first (queued, running), then this process's ended ones and the history on
// disk. enter shows a result (or a running agent's console, or a failure's
// output); k twice cancels; x removes an ended record.

// The ctrl+\ popup's tabs, in the web switcher's order: Agents, AI tasks,
// Open files.
const (
	tabSessions = iota
	tabTasks
	tabFiles
)

// taskRow is one row: a task this process knows, or a history record.
type taskRow struct {
	live   *domain.TaskInfo
	record *domain.TaskRecord
}

func (r taskRow) id() domain.TaskID {
	if r.live != nil {
		return r.live.ID
	}
	return domain.TaskID(r.record.ID)
}

func (r taskRow) key() string {
	if r.live != nil {
		return r.live.Key
	}
	return r.record.Key
}

func (r taskRow) agent() string {
	if r.live != nil {
		return r.live.Agent
	}
	return r.record.Agent
}

func (r taskRow) kind() exttool.Category {
	if r.live != nil {
		return r.live.Kind
	}
	return exttool.Category(r.record.Kind)
}

func (r taskRow) worktree() string {
	if r.live != nil {
		return r.live.Worktree
	}
	return r.record.Worktree
}

func (r taskRow) state() domain.TaskState {
	if r.live != nil {
		return r.live.State
	}
	return domain.TaskState(r.record.State)
}

// taskRows orders the tab: List() (live tasks, then this process's ended
// ones), then history records List() does not hold; filtered by query and
// without the ids x removed.
func taskRows(list []domain.TaskInfo, hist []domain.TaskRecord, query string, removed map[domain.TaskID]bool) []taskRow {
	q := strings.ToLower(query)
	keep := func(r taskRow) bool {
		if removed[r.id()] {
			return false
		}
		return q == "" || strings.Contains(strings.ToLower(r.key()+" "+r.agent()), q)
	}
	var out []taskRow
	seen := map[domain.TaskID]bool{}
	for i := range list {
		seen[list[i].ID] = true
		if r := (taskRow{live: &list[i]}); keep(r) {
			out = append(out, r)
		}
	}
	for i := range hist {
		if seen[domain.TaskID(hist[i].ID)] {
			continue
		}
		if r := (taskRow{record: &hist[i]}); keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// taskStateLabel translates a task state.
func taskStateLabel(s domain.TaskState) string {
	switch s {
	case domain.TaskQueued:
		return i18n.T("queued")
	case domain.TaskRunning:
		return i18n.T("running")
	case domain.TaskResultReady:
		return i18n.T("result ready")
	case domain.TaskDone:
		return i18n.T("done")
	case domain.TaskFailed:
		return i18n.T("failed")
	case domain.TaskCancelled:
		return i18n.T("cancelled")
	}
	return string(s)
}

// taskRowCells is a row's table cells: key, agent, state, age.
func taskRowCells(r taskRow, now time.Time) [4]string {
	var age string
	switch {
	case r.live != nil && r.live.State.Live():
		since := r.live.Started
		if since.IsZero() {
			since = r.live.Submitted
		}
		age = formatElapsed(now.Sub(since))
	case r.live != nil:
		age = finishedAt(r.live.Ended, now)
	default:
		age = finishedAt(r.record.Ended, now)
	}
	return [4]string{r.key(), r.agent(), taskStateLabel(r.state()), age}
}

// finishedAt is a finished task's time: its local date and time, then how
// long ago in its largest unit only — a settled row does not tick.
func finishedAt(ended, now time.Time) string {
	return ended.Local().Format("01-02 15:04") + " · " + i18n.T("%s ago", coarseAgo(now.Sub(ended)))
}

// coarseAgo is d in its largest whole unit: 3h15m3s → 3h, 3m15s → 3m, 55s.
func coarseAgo(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	case d >= time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d >= time.Minute:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return strconv.Itoa(max(int(d/time.Second), 0)) + "s"
}

// taskRowText is "key · agent · state · age" (the quit popup and tests).
func taskRowText(r taskRow, now time.Time) string {
	c := taskRowCells(r, now)
	return strings.Join(c[:], " · ")
}

// openSessionsPopupOn opens the ctrl+\ popup on tab with the cursor on task
// id (the Worktrees ◆ rows' enter).
func (m Model) openSessionsPopupOn(tab int, id domain.TaskID) (Model, tea.Cmd) {
	p := &sessionsPopup{tab: tab}
	p.hist = domain.Tasks().History()
	p.refresh(m)
	p.sel = p.nextSelectable(-1, +1)
	for i, r := range p.taskRows {
		if r.id() == id {
			p.taskSel = i
		}
	}
	return m.pushLayer(p), nil
}

// currentTask is the task row under the tab's cursor.
func (p *sessionsPopup) currentTask() (taskRow, bool) {
	if p.taskSel < 0 || p.taskSel >= len(p.taskRows) {
		return taskRow{}, false
	}
	return p.taskRows[p.taskSel], true
}

// updateTasks handles the Headless tab's own keys.
func (p *sessionsPopup) updateTasks(m Model, key string) (Model, tea.Cmd) {
	if key != "k" {
		p.confirmCancel = ""
	}
	switch key {
	case "up", "ctrl+p":
		if p.taskSel > 0 {
			p.taskSel--
		}
	case "down", "ctrl+n", "j":
		if p.taskSel < len(p.taskRows)-1 {
			p.taskSel++
		}
	case "enter":
		r, ok := p.currentTask()
		if !ok {
			return m, nil
		}
		return p.openTask(m, r)
	case "k":
		r, ok := p.currentTask()
		if !ok || !r.state().Live() {
			return m, nil
		}
		if p.confirmCancel != r.id() {
			p.confirmCancel = r.id()
			m.statusMsg = i18n.T("press k again to cancel %s", r.key())
			return m, nil
		}
		p.confirmCancel = ""
		_ = domain.Tasks().Cancel(r.id())
		m.statusMsg = i18n.T("cancelled %s", r.key())
	case "s":
		r, ok := p.currentTask()
		if !ok || !r.saveFailed() {
			return m, nil
		}
		if err := domain.Tasks().RetrySave(r.id()); err != nil {
			m.statusMsg = err.Error()
		} else {
			m.statusMsg = i18n.T("review saved")
		}
		p.hist = domain.Tasks().History()
	case "x":
		r, ok := p.currentTask()
		if !ok {
			return m, nil
		}
		if r.state().Live() {
			m.statusMsg = i18n.T("cancel it first (k k)")
			return m, nil
		}
		_ = domain.Tasks().RemoveHistory(string(r.id()))
		m = m.ensureTaskTrack()
		m.taskTrack.removed[r.id()] = true
		p.hist = domain.Tasks().History()
	}
	p.refresh(m)
	return m, nil
}

// openTask is enter on a task: its result, else a running agent's console,
// else a failure's output.
func (p *sessionsPopup) openTask(m Model, r taskRow) (Model, tea.Cmd) {
	if r.kind() == exttool.CatReview {
		// A commit/range/branch review lives in its note, never in a file.
		if id := r.noteID(); id != "" {
			label := strings.TrimPrefix(r.key(), "review — ")
			return m.openReview(id, reviewTitle(label))
		}
		if r.live != nil && r.live.SaveErr != "" {
			m.statusMsg = i18n.T("%s — [s] retry save", r.live.SaveErr)
			return m, nil
		}
	}
	result := ""
	if r.live != nil && r.live.Results > 0 {
		result = r.live.Result
	} else if !r.state().Live() {
		result, _ = domain.Tasks().HistoryResult(string(r.id()))
	}
	if strings.TrimSpace(result) != "" {
		ext := ".md"
		var apply func(Model) (Model, tea.Cmd)
		if r.kind() == exttool.CatCommitMessage {
			ext = ".txt"
			if domain.SameCheckout(r.worktree(), m.currentWorktree) {
				text, from := result, r.agent()
				apply = func(m Model) (Model, tea.Cmd) {
					m = m.clearLayers()
					m.pendingCommitMsg[pendingKey(m.currentWorktree)] = pendingMessage{text: text, from: from}
					return m.openCommitBox(), nil
				}
			}
		}
		return m.openResultViewer(r.id(), ext, r.key(), result, apply)
	}
	if r.live != nil && r.live.Session != "" && r.live.State.Live() {
		m = m.popLayer()
		return m.openConsole(r.live.Session)
	}
	if r.state() == domain.TaskFailed {
		tail := ""
		if r.live != nil {
			tail = r.live.Tail
		}
		if tail == "" {
			tail, _ = domain.Tasks().HistoryTail(string(r.id()))
		}
		if strings.TrimSpace(tail) == "" && r.live != nil {
			tail = r.live.Err
		}
		if strings.TrimSpace(tail) == "" && r.record != nil {
			tail = r.record.Err
		}
		return m.openResultViewer(r.id()+"-output", ".log", i18n.T("%s — output", r.key()), tail, nil)
	}
	m.statusMsg = i18n.T("no result yet")
	return m, nil
}

// renderTaskRows lays the tab out as a table — a dim header, then one row
// per task in rowsH lines: task · agent · state · age, the columns sized to
// their widest cell over every row (so scrolling never shifts them) and the
// task key taking what is left.
func (p *sessionsPopup) renderTaskRows(m Model, textW, rowsH int) []string {
	s := st()
	head := []string{i18n.T("Task"), i18n.T("Agent"), i18n.T("State"), i18n.T("Age")}
	now := time.Now()
	cells := make([][4]string, len(p.taskRows))
	ws := [4]int{}
	for c := 1; c < 4; c++ {
		ws[c] = lipgloss.Width(head[c])
	}
	for i, r := range p.taskRows {
		cells[i] = taskRowCells(r, now)
		for c := 1; c < 4; c++ {
			ws[c] = max(ws[c], lipgloss.Width(cells[i][c]))
		}
	}
	const gap = "  "
	ws[0] = max(textW-2-ws[1]-ws[2]-ws[3]-3*len(gap), 8)
	line := func(c [4]string) string {
		return padRight(truncate(c[0], ws[0]), ws[0]) + gap + padRight(truncate(c[1], ws[1]), ws[1]) + gap +
			padRight(c[2], ws[2]) + gap + c[3]
	}
	out := []string{s.dim.Render(padRight("  "+line([4]string{head[0], head[1], head[2], head[3]}), textW))}
	if len(p.taskRows) == 0 {
		return append(out, padRight(i18n.T("  (no AI tasks)"), textW))
	}
	wr := make([]winRow, len(p.taskRows))
	for i := range p.taskRows {
		text, style := "  "+line(cells[i]), lipgloss.Style{}
		if i == p.taskSel {
			text, style = "> "+line(cells[i]), s.selectedRow
		}
		wr[i] = winRow{text: text, style: style}
	}
	return append(out, renderWindow(wr, winOpts{w: textW, h: rowsH, mode: p.mode, anchor: p.taskSel, hscroll: p.hscroll})...)
}

// taskHint is the tab's key line for the row under the cursor: enter only
// when it opens something (a result, a running agent's console, a failure's
// output — a queued or running headless task has none), k k only for a live
// task, x only for a finished one.
func (p *sessionsPopup) taskHint() string {
	var parts []string
	if r, ok := p.currentTask(); ok {
		switch {
		case r.hasResult():
			parts = append(parts, i18n.T("[enter] result"))
		case r.live != nil && r.live.State.Live() && r.live.Session != "":
			parts = append(parts, i18n.T("[enter] console"))
		case r.state() == domain.TaskFailed:
			parts = append(parts, i18n.T("[enter] output"))
		}
		if r.saveFailed() {
			parts = append(parts, i18n.T("[s] retry save"))
		}
		if r.state().Live() {
			parts = append(parts, i18n.T("[k k] cancel"))
		} else {
			parts = append(parts, i18n.T("[x] remove"))
		}
	}
	parts = append(parts, p.tasksTabHint())
	return strings.Join(parts, "  ")
}

// tasksTabHint names where tab goes next: Open files, or Agents in quit mode
// (which has no Open files tab).
func (p *sessionsPopup) tasksTabHint() string {
	if p.quitMode {
		return i18n.T("[/] filter  [tab] sessions  [ctrl+t] full  [esc] close")
	}
	return i18n.T("[/] filter  [tab] open files  [ctrl+t] full  [esc] close")
}

// noteID is the note a review run was stored as ("" = none).
func (r taskRow) noteID() string {
	if r.live != nil {
		return r.live.NoteID
	}
	if r.record != nil {
		return r.record.NoteID
	}
	return ""
}

// saveFailed reports a review run whose result could not be stored.
func (r taskRow) saveFailed() bool { return r.live != nil && r.live.SaveErr != "" }

// hasResult reports a row enter shows a result for — without reading the
// history's files (the hint is drawn every frame).
func (r taskRow) hasResult() bool {
	if r.noteID() != "" {
		return true
	}
	if r.saveFailed() {
		return false
	}
	if r.live != nil {
		return r.live.Results > 0
	}
	return r.record.ResultFile != ""
}
