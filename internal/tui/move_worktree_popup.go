package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// moveWorktreePopup collects the destination for engine.MoveWorktree. The
// rename face edits just the directory NAME (dest joins the old parent); the
// move face edits the full absolute path. Enter dispatches directly — this
// popup IS the confirmation; esc cancels. Mirrors checkoutAsPopup.
type moveWorktreePopup struct {
	popupMax
	wt     model.Worktree
	rename bool
	field  textfield
}

func (p *moveWorktreePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		val := strings.TrimSpace(p.field.Value())
		if val == "" {
			return m, nil
		}
		dest := val
		if p.rename {
			if strings.ContainsAny(val, `/\`) {
				m.statusMsg = i18n.T("a new name cannot contain a path separator (use Move worktree…)")
				return m, nil
			}
			dest = filepath.Join(filepath.Dir(p.wt.Path), val)
		}
		if filepath.Clean(dest) == filepath.Clean(p.wt.Path) {
			return m.popLayer(), nil // unchanged: no-op
		}
		wt := p.wt
		m = m.popLayer()
		if wt.Path == m.currentWorktree {
			// gg's own cwd must leave the tree before git renames it (Windows
			// cannot rename a directory any process holds as cwd); the chained
			// reRoot below lands us in the new path.
			_ = os.Chdir(filepath.Dir(wt.Path))
			m.pendingSwitch = true // opFinishedMsg chains guardedReRoot(res.Path)
		}
		m.pendingWorktreeMoveOld = wt.Path
		return m.startOp(engine.MoveWorktree{Path: wt.Path, Dest: dest})
	default:
		p.field.HandleEditKey(msg)
	}
	return m, nil
}

func (p *moveWorktreePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	title := i18n.T("Move worktree %s", p.wt.Path)
	label := i18n.T("path: ")
	verb := i18n.T("[enter] move   [esc] cancel")
	if p.rename {
		title = i18n.T("Rename worktree %s", filepath.Base(p.wt.Path))
		label = i18n.T("name: ")
		verb = i18n.T("[enter] rename   [esc] cancel")
	}
	b.WriteString(title + "\n\n")
	b.WriteString(viewField(label, p.field, true, popupContentWidth(w)) + "\n\n")
	b.WriteString(verb)
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// openMoveWorktreePopup pushes the rename/move popup for wt; rename prefills
// the basename, move the full path.
func (m Model) openMoveWorktreePopup(wt model.Worktree, rename bool) Model {
	prefill := wt.Path
	if rename {
		prefill = filepath.Base(wt.Path)
	}
	return m.pushLayer(&moveWorktreePopup{wt: wt, rename: rename, field: newTextField(prefill)})
}

// moveWorktreeRow offers "Move worktree…" on the Worktrees panel: a menu-only
// row (no replayable key, like tagCheckoutRow) that opens the popup's move
// face (full-path field), gated the same as e's rename face.
func (m Model) moveWorktreeRow() (actionRow, bool) {
	if m.focus != panelWorktrees || !m.canMoveWorktree() {
		return actionRow{}, false
	}
	wt, ok := m.selectedWorktree()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "move-worktree",
		label: i18n.T("Move worktree…"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openMoveWorktreePopup(wt, false), nil
		},
	}, true
}
