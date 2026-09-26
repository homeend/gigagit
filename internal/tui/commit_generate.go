package tui

import (
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
)

// commit_generate.go is the commit box's ctrl+g: a commit-message AI task
// launched through the task launch dialog (task_launch_popup.go). A
// headless run started from the box keeps the box waiting on it (spinner,
// esc cancels, b sends it to the background); every commit-message
// result — any mode, any launch — lands through applyCommitMessage: into the
// open box (asking before it replaces text), else as the worktree's pending
// message the next c opens with.

// pendingMessage is a commit-message result that arrived while no commit
// box was open for its worktree.
type pendingMessage struct {
	text string
	from string // the agent's display name
}

// startGenerate opens the launch dialog over the commit box.
func (m Model) startGenerate(p *commitPopup) (Model, tea.Cmd) {
	if m.status.Counts().Staged == 0 {
		m.statusMsg = i18n.T("nothing staged to describe")
		return m, nil
	}
	return m.openTaskLaunch(taskLaunch{kind: exttool.CatCommitMessage, commitBox: true})
}

// commitBoxWaitOn makes the commit box on top wait on headless task id.
func (m Model) commitBoxWaitOn(id domain.TaskID) (Model, tea.Cmd) {
	p := m.topCommitPopup()
	if p == nil {
		return m, nil
	}
	p.genTask = id
	p.generating = true
	p.genGen++
	p.spinFrame = 0
	p.genStart = time.Now()
	return m, spinTickCmd(p.genGen)
}

// genSpinMsg advances the in-flight generate spinner; gen guards it against a
// finished or superseded run (the noticeBlink self-stopping-tick pattern).
type genSpinMsg struct{ gen int }

// spinTickCmd schedules the next spinner frame ~100ms out.
func spinTickCmd(gen int) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return genSpinMsg{gen: gen} })
}

// tickGenSpinner advances the spinner and reschedules while a matching run is
// in flight; a stale/finished run stops the tick.
func (m Model) tickGenSpinner(msg genSpinMsg) (Model, tea.Cmd) {
	p := m.topCommitPopup()
	if p == nil || !p.generating || msg.gen != p.genGen {
		return m, nil
	}
	if info, ok := domain.Tasks().Get(p.genTask); ok && !info.State.Live() {
		p.generating = false // ended without a result: taskEnded says why
		return m, nil
	}
	p.spinFrame++
	return m, spinTickCmd(msg.gen)
}

// escGenerate cancels the box's own task (esc keeps its meaning: stop).
func (m Model) escGenerate(p *commitPopup) Model {
	if p.genTask != "" {
		_ = domain.Tasks().Cancel(p.genTask)
	}
	p.genTask = ""
	p.genGen++
	p.generating = false
	return m
}

// backgroundGenerate closes the box and leaves its task running; the
// result becomes the pending message.
func (m Model) backgroundGenerate(p *commitPopup) Model {
	p.generating = false
	p.genGen++
	m = m.removeLayer(p)
	m.statusMsg = i18n.T("the commit message is generated in the background — press c when it is ready")
	return m
}

// fill replaces the box's fields with msg.
func (p *commitPopup) fill(msg string) {
	subject, body := splitMessage(msg)
	p.title = newTextField(subject)
	p.desc = newTextField(body)
	p.field = 0
	p.offer, p.offerFrom = "", ""
}

// applyCommitMessage lands a commit-message result: into the open commit
// box of its checkout (asking first when the box has text), else as the
// worktree's pending message.
func (m Model) applyCommitMessage(info domain.TaskInfo) (Model, tea.Cmd) {
	if !m.taskHere(info) {
		return m.stickyNotice(i18n.T("%s ready — ctrl+\\", info.Key))
	}
	if p := m.topCommitPopup(); p != nil && !p.amend {
		if p.genTask == info.ID {
			p.generating, p.genTask = false, ""
			p.genGen++
		}
		if strings.TrimSpace(p.title.Value()) == "" && strings.TrimSpace(p.desc.Value()) == "" {
			p.fill(info.Result)
			m.statusMsg = i18n.T("message from %s", info.Agent)
			return m, nil
		}
		p.offer, p.offerFrom = info.Result, info.Agent
		return m, nil
	}
	m.pendingCommitMsg[pendingKey(info.Worktree)] = pendingMessage{text: info.Result, from: info.Agent}
	return m.stickyNotice(i18n.T("commit message from %s ready — press c", info.Agent))
}

// pendingKey is a worktree's key in Model.pendingCommitMsg.
func pendingKey(worktree string) string { return filepath.Clean(worktree) }

// openCommitBox opens the commit popup, filled from this worktree's pending
// message when one waits.
func (m Model) openCommitBox() Model {
	p := &commitPopup{}
	key := pendingKey(m.currentWorktree)
	if pm, ok := m.pendingCommitMsg[key]; ok && m.currentWorktree != "" {
		delete(m.pendingCommitMsg, key)
		p.fill(pm.text)
		m.statusMsg = i18n.T("message from %s", pm.from)
	}
	return m.pushLayer(p)
}

// topCommitPopup returns the active commit popup if it is the topmost layer,
// else nil.
func (m Model) topCommitPopup() *commitPopup {
	if p, ok := m.topLayer().(*commitPopup); ok {
		return p
	}
	return nil
}

// updateOffer drives the ask-before-replace box for a result that arrived
// while the box had text: y/enter replaces, esc/n keeps the current text.
func (p *commitPopup) updateOffer(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		from := p.offerFrom
		p.fill(p.offer)
		m.statusMsg = i18n.T("message from %s", from)
	case "n", "esc":
		p.offer, p.offerFrom = "", ""
	}
	return m, nil
}

// offerBox renders the replace-existing-text question.
func (p *commitPopup) offerBox(m Model) string {
	w, _ := m.overlayDims()
	subject, _ := splitMessage(p.offer)
	content := i18n.T("Replace current message with %s's?", p.offerFrom) + "\n\n" +
		truncate(subject, popupTextWidth(popupInnerWidth(w))) + "\n\n" +
		i18n.T("[y]es / [enter]  [esc] no")
	return st().modalStyle.Width(popupInnerWidth(w)).Render(content) + "\n"
}
