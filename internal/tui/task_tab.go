package tui

import (
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

const (
	tabSessions = iota
	tabTasks
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

// taskRowText is "key · agent · state · age".
func taskRowText(r taskRow, now time.Time) string {
	var age string
	switch {
	case r.live != nil && r.live.State.Live():
		since := r.live.Started
		if since.IsZero() {
			since = r.live.Submitted
		}
		age = formatElapsed(now.Sub(since))
	case r.live != nil:
		age = i18n.T("%s ago", formatElapsed(now.Sub(r.live.Ended)))
	default:
		age = i18n.T("%s ago", formatElapsed(now.Sub(r.record.Ended)))
	}
	return r.key() + " · " + r.agent() + " · " + taskStateLabel(r.state()) + " · " + age
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
	result := ""
	if r.live != nil && r.live.Results > 0 {
		result = r.live.Result
	} else if !r.state().Live() {
		result, _ = domain.Tasks().HistoryResult(string(r.id()))
	}
	if strings.TrimSpace(result) != "" {
		v := newReviewView(r.key(), "", result)
		v.copyText = result
		if r.kind() == exttool.CatCommitMessage && domain.SameCheckout(r.worktree(), m.currentWorktree) {
			text, from := result, r.agent()
			v.apply = func(m Model) (Model, tea.Cmd) {
				m = m.clearLayers()
				m.pendingCommitMsg[pendingKey(m.currentWorktree)] = pendingMessage{text: text, from: from}
				return m.openCommitBox(), nil
			}
		}
		return m.pushLayer(v), nil
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
		v := newReviewView(i18n.T("%s — output", r.key()), "", tail)
		v.copyText = tail
		return m.pushLayer(v), nil
	}
	m.statusMsg = i18n.T("no result yet")
	return m, nil
}

// renderTaskRows lays out the tab's rows.
func (p *sessionsPopup) renderTaskRows(m Model, textW, h int) []string {
	if len(p.taskRows) == 0 {
		return []string{padRight(i18n.T("  (no AI tasks)"), textW)}
	}
	s := st()
	now := time.Now()
	wr := make([]winRow, len(p.taskRows))
	for i, r := range p.taskRows {
		text := "  " + taskRowText(r, now)
		var style lipgloss.Style
		if i == p.taskSel {
			text, style = "> "+taskRowText(r, now), s.selectedRow
		}
		wr[i] = winRow{text: text, style: style}
	}
	rowsH := min(len(wr), max(h-10, 3))
	return renderWindow(wr, winOpts{w: textW, h: rowsH, mode: p.mode, anchor: p.taskSel, hscroll: p.hscroll})
}
