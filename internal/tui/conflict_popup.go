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
	inProgress string   // "merge"/"rebase"/"" — gates the continue/abort actions
	mode       dispMode // text display mode; z cycles (cutoff default)
	hscroll    int      // modeScroll horizontal offset
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
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	var b strings.Builder
	b.WriteString("Resolve conflicts\n")
	if src := m.conflict.Describe(); src != "" {
		b.WriteString(conflictSrcStyle.Render(src) + "\n")
	}
	b.WriteString("\n")
	if len(p.files) == 0 {
		b.WriteString("  (all resolved)\n")
	} else {
		wr := make([]winRow, len(p.files))
		for i, f := range p.files {
			prefix := "  "
			var st lipgloss.Style
			if i == p.sel {
				prefix, st = "> ", selectedRow
			}
			wr[i] = winRow{text: fmt.Sprintf("%s%s  — %s", prefix, f.Path, f.ConflictLabel()), style: st}
		}
		// Box grows with the file count up to 12 rows; renderWindow then scrolls
		// to keep p.sel visible. Styling is applied after truncate/wrap.
		h := len(p.files)
		if h > 12 {
			h = 12
		}
		for _, line := range renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll}) {
			b.WriteString(line + "\n")
		}
	}
	// The hint is longer than the box, so wrap it across lines (every key must
	// stay visible) instead of letting popupBox truncate it.
	hintParts := append(p.actionHintParts(), "[esc] close", "[z] mode")
	b.WriteString("\n" + strings.Join(wrapParts(hintParts, textW, "  "), "\n"))
	return popupBox(inner, b.String())
}

// actionHintParts lists the keys available for the selected file (+ continue/
// abort) as separate parts, so the renderer can wrap them across lines instead
// of truncating the (long) hint at the box edge.
func (p *conflictPopup) actionHintParts() []string {
	// All conflicts resolved: either offer continue/abort (op in progress) or
	// tell the user to commit (no op — e.g. a stash-pop conflict).
	if len(p.files) == 0 {
		if p.inProgress != "" {
			return []string{"[c] continue " + p.inProgress, "[a] abort"}
		}
		// No merge/rebase to continue (e.g. a stash-pop conflict): the resolved
		// changes are staged, so the user closes the popup and commits with c.
		return []string{"all resolved — press [esc], then [c] to commit"}
	}
	var parts []string
	if p.sel >= 0 && p.sel < len(p.files) {
		f := p.files[p.sel]
		if f.ConflictClass() == model.ConflictBothSides {
			parts = append(parts, "[enter] pick hunks", "[o] current", "[i] incoming", "[m] mark resolved")
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
	return parts
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
	case "z": // cycle the text display mode (cutoff / wrap / scroll)
		p.mode = p.mode.next()
		p.hscroll = 0
		return m, nil
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
		return m, nil
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
	case "enter":
		if p.sel < 0 || p.sel >= len(p.files) {
			return m, nil
		}
		f := p.files[p.sel]
		if f.ConflictClass() != model.ConflictBothSides {
			m.statusMsg = "hunk picker: only for files modified on both sides"
			return m, nil
		}
		return m, m.loadConflictFileCmd(f.Path)
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
	case "i":
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
