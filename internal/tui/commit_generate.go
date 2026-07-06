package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/template"
)

// genMessageMsg carries the result of a headless commit_message agent run
// (see genMessageCmd). gen is the commitPopup.genGen at dispatch time — a
// stale or cancelled run's result is dropped by applyGeneratedMessage when
// it no longer matches the live popup's genGen.
type genMessageMsg struct {
	gen     int
	subject string
	body    string
	err     error
}

// startGenerate begins a commit_message capture run for the commit popup.
//
// Scope seam for Task 7: this task assumes a SINGLE, already-approved
// commit_message tool (cmds[0]). Task 7 inserts the chooser (len(cmds) > 1),
// first-run approval, and confirm-replace-existing-text gates between the
// guards below and the dispatchGenerate call.
func (m Model) startGenerate(p *commitPopup) (Model, tea.Cmd) {
	if m.status.Counts().Staged == 0 {
		m.statusMsg = "nothing staged to describe"
		return m, nil
	}
	cmds := m.toolCommands(string(exttool.CatCommitMessage))
	if len(cmds) == 0 {
		m.statusMsg = "no commit-message tool configured (Settings → External tools)"
		return m, nil
	}
	chosen := cmds[0] // Task 7: chooser when len(cmds) > 1
	resolved, err := template.ResolveCommand(chosen.Command, nil, template.CmdCtx{Repo: m.currentWorktree})
	if err != nil {
		m.statusMsg = "generate: " + err.Error()
		return m, nil
	}
	p.genCmd = chosen
	return m.dispatchGenerate(p, resolved)
}

// dispatchGenerate arms the run (Task 7's gates call this once approved/confirmed).
func (m Model) dispatchGenerate(p *commitPopup, resolvedCommand string) (Model, tea.Cmd) {
	p.generating = true
	p.genGen++
	ctx, cancel := context.WithCancel(context.Background())
	m.genCancel = cancel
	return m, m.genMessageCmd(resolvedCommand, p.genGen, ctx)
}

// genMessageCmd runs the resolved command headless via the Task-3 engine op,
// synchronously inside the returned tea.Cmd (the stageCmd/gitConfigWriteCmd
// pattern), then parses the captured stdout into a subject/body pair.
func (m Model) genMessageCmd(command string, gen int, ctx context.Context) tea.Cmd {
	svc, dir := m.svc, m.currentWorktree
	return func() tea.Msg {
		op := engine.GenerateMessage{Command: command, Dir: dir, Env: []string{"GG_TASK=commit_message"}}
		res, err := svc.Execute(ctx, op, nil, nil)
		if err != nil {
			return genMessageMsg{gen: gen, err: err}
		}
		subject, body, perr := exttool.ParseCaptureMessage([]byte(res.Captured))
		if perr != nil {
			return genMessageMsg{gen: gen, err: perr}
		}
		return genMessageMsg{gen: gen, subject: subject, body: body}
	}
}

// applyGeneratedMessage fills the live commit popup, gen-guarded: a result
// whose gen no longer matches the popup's current genGen (a stale run, an
// esc-cancelled run, a closed popup, or a repo switch) is dropped silently.
func (m Model) applyGeneratedMessage(msg genMessageMsg) Model {
	p := m.topCommitPopup()
	if p == nil || msg.gen != p.genGen {
		return m // stale / popup closed / repo switched / cancelled
	}
	p.generating = false
	m.genCancel = nil
	if msg.err != nil {
		m.statusMsg = "generate: " + msg.err.Error()
		return m
	}
	p.title = newTextField(msg.subject)
	p.desc = newTextField(msg.body)
	p.field = 0
	return m
}

// escGenerate cancels an in-flight generate run. Bumping genGen is essential:
// a ctx-killed subprocess returns from svc.Execute as *exec.ExitError
// ("signal: killed"), NOT context.Canceled, so applyGeneratedMessage's gen
// guard — not an errors.Is(err, context.Canceled) check — is what drops the
// late error result. Without the bump, every deliberate esc would surface a
// spurious "generate: signal: killed" status message.
func (m Model) escGenerate(p *commitPopup) Model {
	if m.genCancel != nil {
		m.genCancel()
		m.genCancel = nil
	}
	p.genGen++
	p.generating = false
	return m
}

// topCommitPopup returns the active commit popup if it is the topmost layer,
// else nil.
func (m Model) topCommitPopup() *commitPopup {
	if p, ok := m.topLayer().(*commitPopup); ok {
		return p
	}
	return nil
}
