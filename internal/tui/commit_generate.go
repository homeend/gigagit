package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
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

// startGenerate begins a commit_message capture run for the commit popup,
// gated by three sub-states run in order (each a commitPopup field, not a
// pushed layer — see the routing in commit_popup.go's update):
//
//  1. Chooser (len(cmds) > 1): p.choosing holds the candidates; a digit/enter
//     selection (updateChoosing) picks one and continues at gateGenerate.
//  2. First-run approval: an unapproved command (by config-text hash) sets
//     p.approving to the resolved command text; y/enter (updateApproving)
//     records the approval and continues at confirmGenerate; esc/n cancels.
//  3. Confirm-replace: non-empty existing title/desc sets p.confirming; y/enter
//     (updateConfirming) dispatches; esc cancels. Empty fields skip straight
//     to dispatchGenerate.
func (m Model) startGenerate(p *commitPopup) (Model, tea.Cmd) {
	if m.reviewRunning {
		m.statusMsg = i18n.T("a review is in progress — wait for it to finish")
		return m, nil
	}
	if m.status.Counts().Staged == 0 {
		m.statusMsg = i18n.T("nothing staged to describe")
		return m, nil
	}
	cmds := m.toolCommands(string(exttool.CatCommitMessage))
	if len(cmds) == 0 {
		m.statusMsg = i18n.T("no commit-message tool configured (Settings → External tools)")
		return m, nil
	}
	if len(cmds) > 1 {
		p.choosing = cmds
		return m, nil
	}
	return m.gateGenerate(p, cmds[0])
}

// gateGenerate resolves chosen and applies the first-run approval gate
// (step 2). Approval is remembered on chosen.Command — the CONFIG template
// text — never the resolved text, so the promptstate hash stays stable
// across runs with different staged diffs (mirrors conflict_process.go's
// gateOrRun/updateToolApprove).
func (m Model) gateGenerate(p *commitPopup, chosen config.ToolCommand) (Model, tea.Cmd) {
	resolved, err := template.ResolveCommand(chosen.Command, nil, template.CmdCtx{Repo: m.currentWorktree})
	if err != nil {
		m.statusMsg = i18n.T("generate: %s", err.Error())
		return m, nil
	}
	p.genCmd = chosen
	if !m.toolCommandApproved(chosen.Command) {
		p.approving = resolved
		return m, nil
	}
	return m.confirmGenerate(p, resolved)
}

// confirmGenerate applies the confirm-replace gate (step 3): existing
// title/desc text asks before overwriting it; empty fields dispatch
// straight through.
func (m Model) confirmGenerate(p *commitPopup, resolved string) (Model, tea.Cmd) {
	if strings.TrimSpace(p.title.Value()) != "" || strings.TrimSpace(p.desc.Value()) != "" {
		p.confirming = resolved
		return m, nil
	}
	return m.dispatchGenerate(p, resolved)
}

// dispatchGenerate arms the run (Task 7's gates call this once approved/confirmed).
func (m Model) dispatchGenerate(p *commitPopup, resolvedCommand string) (Model, tea.Cmd) {
	p.generating = true
	p.genGen++
	p.spinFrame = 0
	p.genStart = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	m.genCancel = cancel
	// Batch the headless run with an animated-spinner tick so the popup shows
	// visible motion (not a frozen screen) while the agent works.
	return m, tea.Batch(m.genMessageCmd(resolvedCommand, p.genGen, ctx), spinTickCmd(p.genGen))
}

// genSpinMsg advances the in-flight generate spinner; gen guards it against a
// finished or superseded run (the noticeBlink self-stopping-tick pattern).
type genSpinMsg struct{ gen int }

// spinTickCmd schedules the next spinner frame ~100ms out.
func spinTickCmd(gen int) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return genSpinMsg{gen: gen} })
}

// tickGenSpinner advances the spinner and reschedules while a matching run is
// in flight; a stale/finished run stops the tick (no reschedule), so the
// animation self-terminates when generation ends, is cancelled, or the popup
// closes.
func (m Model) tickGenSpinner(msg genSpinMsg) (Model, tea.Cmd) {
	p := m.topCommitPopup()
	if p == nil || !p.generating || msg.gen != p.genGen {
		return m, nil
	}
	p.spinFrame++
	return m, spinTickCmd(msg.gen)
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
		m.statusMsg = i18n.T("generate: %s", msg.err.Error())
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

// --- Task 7 gate 1: chooser (len(cmds) > 1) ---

// updateChoosing drives the tool chooser: a digit 1-9 selects that row
// directly, enter selects the first (default) row, esc cancels back to plain
// editing. Unlike the conflict lane's up/down list (conflictProcess's
// confToolPick), this list is small and numbered, so a direct digit press is
// the primary path — no cursor state to track.
func (p *commitPopup) updateChoosing(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		p.choosing = nil
		return m, nil
	case tea.KeyEnter:
		return p.selectChosen(m, 0)
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			if r >= '1' && r <= '9' {
				return p.selectChosen(m, int(r-'1'))
			}
		}
	}
	return m, nil
}

// selectChosen picks choosing[idx] (a no-op on an out-of-range index — e.g. a
// digit beyond the list length) and continues at the approval gate.
func (p *commitPopup) selectChosen(m Model, idx int) (Model, tea.Cmd) {
	if idx < 0 || idx >= len(p.choosing) {
		return m, nil
	}
	chosen := p.choosing[idx]
	p.choosing = nil
	return m.gateGenerate(p, chosen)
}

// chooseBox renders the numbered tool picker.
func (p *commitPopup) chooseBox(m Model) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString("Choose a commit-message tool\n\n")
	for i, tc := range p.choosing {
		b.WriteString(fmt.Sprintf("[%d] %s\n", i+1, tc.Name))
	}
	b.WriteString("\n[1-9] choose  [enter] first  [esc] cancel")
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}

// --- Task 7 gate 2: first-run approval ---

// updateApproving drives the first-run approval box: y/enter records the
// approval (on the CONFIG command text, p.genCmd.Command — see gateGenerate)
// and proceeds to the confirm-replace gate; esc/n cancels back to plain
// editing without dispatching or recording anything.
func (p *commitPopup) updateApproving(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		p.approving = ""
		return m, nil
	case tea.KeyEnter:
		return p.approveAndProceed(m)
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "y":
			return p.approveAndProceed(m)
		case "n":
			p.approving = ""
			return m, nil
		}
	}
	return m, nil
}

func (p *commitPopup) approveAndProceed(m Model) (Model, tea.Cmd) {
	resolved := p.approving
	p.approving = ""
	m.rememberToolApproval(p.genCmd.Command)
	return m.confirmGenerate(p, resolved)
}

// approveBox renders the shared approval body under a header naming the
// chosen tool, mirroring conflict_process.go's confToolApprove render
// (header + approvalBoxView(...) — see tool_approval.go for why the header
// stays owned by each call site).
func (p *commitPopup) approveBox(m Model) string {
	w, _ := m.overlayDims()
	header := i18n.T("Run this command?  (%s)", p.genCmd.Name) + "\n\n"
	return modalStyle.Width(popupInnerWidth(w)).Render(header+approvalBoxView(p.approving, w)) + "\n"
}

// --- Task 7 gate 3: confirm-replace ---

// updateConfirming drives the confirm-replace box: y/enter dispatches the
// run (replacing the current title/desc once the result lands); esc cancels,
// leaving the existing text untouched.
func (p *commitPopup) updateConfirming(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		p.confirming = ""
		return m, nil
	case tea.KeyEnter:
		return p.dispatchConfirmed(m)
	case tea.KeyRunes:
		if string(msg.Runes) == "y" {
			return p.dispatchConfirmed(m)
		}
	}
	return m, nil
}

func (p *commitPopup) dispatchConfirmed(m Model) (Model, tea.Cmd) {
	resolved := p.confirming
	p.confirming = ""
	return m.dispatchGenerate(p, resolved)
}

// confirmBox renders the replace-existing-text confirmation.
func (p *commitPopup) confirmBox(m Model) string {
	w, _ := m.overlayDims()
	content := "Replace current message?\n\nGenerating will overwrite the title/description below.\n\n[y]es / [enter]  [esc] no"
	return modalStyle.Width(popupInnerWidth(w)).Render(content) + "\n"
}
