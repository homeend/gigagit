package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// conflictSrcStyle dims the "merging X into Y" subtitle in the resolve popup.
var conflictSrcStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// conflictPopup resolves unmerged files at the whole-file level.
type conflictPopup struct {
	files      []model.FileStatus // refreshed from status after each action
	sel        int
	inProgress string // "merge"/"rebase"/"" — gates the continue/abort actions
}

func (m Model) openConflictPopup() (Model, tea.Cmd) {
	files := m.status.Conflicts()
	if len(files) == 0 {
		return m, nil
	}
	m.conflictPopup = &conflictPopup{files: files}
	return m, m.loadInProgressCmd()
}

func (m Model) renderConflictPopup() string {
	p := m.conflictPopup
	var b strings.Builder
	b.WriteString("Resolve conflicts\n")
	if src := m.conflict.Describe(); src != "" {
		b.WriteString(conflictSrcStyle.Render(src) + "\n")
	}
	b.WriteString("\n")
	if len(p.files) == 0 {
		b.WriteString("  (all resolved)\n")
	}
	for i, f := range p.files {
		cursor := "  "
		if i == p.sel {
			cursor = "> "
		}
		row := fmt.Sprintf("%s%s  — %s", cursor, f.Path, f.ConflictLabel())
		if i == p.sel {
			b.WriteString(selectedRow.Render(row) + "\n")
		} else {
			b.WriteString(row + "\n")
		}
	}
	b.WriteString("\n" + p.actionHint() + "\n")
	b.WriteString("[esc] close")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}

// actionHint lists the keys available for the selected file (+ continue/abort).
func (p *conflictPopup) actionHint() string {
	// All conflicts resolved: either offer continue/abort (op in progress) or
	// tell the user to commit (no op — e.g. a stash-pop conflict).
	if len(p.files) == 0 {
		if p.inProgress != "" {
			return strings.Join([]string{"[c] continue " + p.inProgress, "[a] abort"}, "  ")
		}
		// No merge/rebase to continue (e.g. a stash-pop conflict): the resolved
		// changes are staged, so the user closes the popup and commits with c.
		return "all resolved — press [esc], then [c] to commit"
	}
	var parts []string
	if p.sel >= 0 && p.sel < len(p.files) {
		f := p.files[p.sel]
		if f.ConflictClass() == model.ConflictBothSides {
			parts = append(parts, "[o] ours", "[t] theirs", "[m] mark resolved")
		} else {
			// "keep modified" needs a side with content; both-deleted (DD) has
			// neither, so only delete / keep base apply there.
			if f.ConflictHasOurs() || f.ConflictHasTheirs() {
				parts = append(parts, "[k] keep modified")
			}
			parts = append(parts, "[d] delete")
			if f.ConflictHasBase() {
				parts = append(parts, "[b] keep base")
			}
		}
	}
	parts = append(parts, "[A] all resolved")
	if p.inProgress != "" {
		parts = append(parts, "[a] abort")
	}
	return strings.Join(parts, "  ")
}

// inProgressMsg carries the result of the merge/rebase-in-progress probe.
type inProgressMsg struct{ op string }

// loadInProgressCmd probes whether a merge/rebase is in progress so the popup
// can offer continue/abort.
func (m Model) loadInProgressCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		op, _ := svc.InProgressOp(context.Background())
		return inProgressMsg{op: op}
	}
}

// keepModifiedAction maps a modify/delete file to the side that has content.
func keepModifiedAction(f model.FileStatus) engine.ConflictAction {
	if f.ConflictHasTheirs() {
		return engine.KeepTheirs
	}
	return engine.KeepOurs
}

func (m Model) updateConflictPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.conflictPopup
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc":
		m.conflictPopup = nil
		return m, nil
	case "up":
		if p.sel > 0 {
			p.sel--
		}
		return m, nil
	case "down", "j":
		if p.sel < len(p.files)-1 {
			p.sel++
		}
		return m, nil
	case "A":
		var paths []string
		for _, f := range p.files {
			paths = append(paths, f.Path)
		}
		if len(paths) == 0 {
			return m, nil
		}
		m.conflictPopup = nil
		m.reopenConflict = true
		return m.startOp(engine.MarkAllResolved{Paths: paths})
	case "c":
		// Continue is allowed only once every conflict is resolved.
		if p.inProgress != "" && len(p.files) == 0 {
			m.conflictPopup = nil
			return m.startOp(engine.ContinueOp{})
		}
		return m, nil
	case "a":
		if p.inProgress != "" {
			m.conflictPopup = nil
			return m.startOp(engine.AbortOp{})
		}
		return m, nil
	}
	if p.sel < 0 || p.sel >= len(p.files) {
		return m, nil
	}
	f := p.files[p.sel]
	both := f.ConflictClass() == model.ConflictBothSides
	hasSide := f.ConflictHasOurs() || f.ConflictHasTheirs()
	var action engine.ConflictAction
	switch msg.String() {
	case "o":
		if !both {
			return m, nil
		}
		action = engine.KeepOurs
	case "t":
		if !both {
			return m, nil
		}
		action = engine.KeepTheirs
	case "m":
		if !both {
			return m, nil
		}
		action = engine.MarkResolved
	case "k":
		if both || !hasSide { // both-deleted (DD) has no side to keep
			return m, nil
		}
		action = keepModifiedAction(f)
	case "d":
		if both {
			return m, nil
		}
		action = engine.DeleteFile
	case "b":
		if both || !f.ConflictHasBase() {
			return m, nil
		}
		action = engine.KeepBase
	default:
		return m, nil
	}
	m.conflictPopup = nil // reopened after the refresh (Task 7 re-syncs the list)
	m.reopenConflict = true
	return m.startOp(engine.ResolveConflict{Path: f.Path, Action: action})
}
