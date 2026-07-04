package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
)

// resumePromptPopup asks the one continue/abort question after gg detects a
// paused merge/rebase/cherry-pick/revert whose conflicts were all resolved
// outside gg (nothing left unmerged, op never continued). esc = Not now —
// the ⏸ status segment and the x key remain the way back in (never trap the
// user). Continue/abort dispatch the existing generic engine ops.
type resumePromptPopup struct {
	op     string // "merge" | "rebase" | "cherry-pick" | "revert"
	detail string // ConflictState.Describe() at push time; "" when unattributed
	sel    int    // 0 = continue, 1 = abort, 2 = not now
}

// maybeResumePrompt pushes the one-shot continue/abort popup when a status
// arrival observes a paused sequencer op with zero conflicted files. The
// flag is set only when the popup is actually shown — a busy skip (op
// running, another window open) retries on the next status arrival — and it
// re-arms whenever the repo leaves the paused-resolved state, so a NEW pause
// prompts again. Call at every point that assigns m.conflict from a fresh
// status read.
func (m Model) maybeResumePrompt() Model {
	pausedResolved := m.conflict.Op != "" && len(m.status.Conflicts()) == 0
	if !pausedResolved {
		m.resumePromptShown = false
		return m
	}
	if m.resumePromptShown || !m.opsIdle() || m.proc != nil || m.modal != nil ||
		m.actionMenu != nil || m.topLayer() != nil || m.stashView != nil || m.filesView != nil {
		return m
	}
	m.resumePromptShown = true
	return m.pushLayer(&resumePromptPopup{op: m.conflict.Op, detail: m.conflict.Describe()})
}

// options returns the fixed three-choice list, continue first.
func (p *resumePromptPopup) options() []string {
	return []string{"Continue " + p.op, "Abort " + p.op, "Not now"}
}

func (p *resumePromptPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	n := len(p.options())
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc: // esc = Not now — never trap; [x] and the ⏸ segment remain
		return m.popLayer(), nil
	case tea.KeyUp:
		p.sel = (p.sel - 1 + n) % n
		return m, nil
	case tea.KeyDown:
		p.sel = (p.sel + 1) % n
		return m, nil
	case tea.KeyEnter:
		sel := p.sel
		m = m.popLayer()
		switch sel {
		case 0:
			return m.startOp(engine.ContinueOp{})
		case 1:
			return m.startOp(engine.AbortOp{})
		}
		return m, nil
	}
	switch msg.String() { // direct shortcuts, mirroring the conflict process keys
	case "c":
		return m.popLayer().startOp(engine.ContinueOp{})
	case "a":
		return m.popLayer().startOp(engine.AbortOp{})
	}
	return m, nil // swallow everything else — no fallthrough to global keys
}

func (p *resumePromptPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	textW := popupTextWidth(popupInnerWidth(w))
	var b strings.Builder
	b.WriteString("⏸ " + p.op + " paused — all conflicts resolved\n")
	if p.detail != "" {
		b.WriteString(conflictSrcStyle.Render(p.detail) + "\n")
	}
	b.WriteString("\n")
	for _, line := range wrapWidth("Continue the "+p.op+" now, or abort it? You can come back any time with [x].", textW, 1<<20) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
	for i, opt := range p.options() {
		prefix := "  "
		if i == p.sel {
			prefix = "> "
		}
		row := prefix + opt
		if i == p.sel {
			row = selectedRow.Render(row)
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n[↑/↓] select  [enter] choose  [c] continue  [a] abort  [esc] not now")
	box := modalStyle.Width(popupInnerWidth(w)).Render(strings.TrimRight(b.String(), "\n")) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
