package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
)

// hookEditorPopup is the wide multi-line editor for the [worktree]
// post_create_hook script (Settings → "Worktree post-create hook"). Enter
// inserts a newline; Ctrl+S saves to the repo .gg.toml; Esc cancels.
type hookEditorPopup struct {
	buf textfield
}

// openHookEditor pushes the hook editor, seeded with the current script.
func (m Model) openHookEditor() Model {
	return m.pushLayer(&hookEditorPopup{buf: newTextField(m.cfg.Worktree.PostCreateHook)})
}

func (p *hookEditorPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		return m.saveHook(p.buf.Value())
	case tea.KeyEnter:
		p.buf.InsertNewline()
	case tea.KeyUp:
		p.buf.Up()
	case tea.KeyDown:
		p.buf.Down()
	default:
		p.buf.HandleEditKey(msg)
	}
	return m, nil
}

// saveHook persists the script to the repo .gg.toml, updates in-memory config,
// and closes the editor (mirrors saveRefreshInterval's surface behavior).
func (m Model) saveHook(script string) (Model, tea.Cmd) {
	m.cfg.Worktree.PostCreateHook = script
	if m.repoConfigPath == "" {
		m.statusMsg = "hook set (not saved: no repo config path)"
		return m.popLayer(), nil
	}
	if err := config.SetWorktreePostCreateHook(m.repoConfigPath, script); err != nil {
		m.statusMsg = "hook set but not saved: " + err.Error()
	} else {
		m.statusMsg = "post-create hook saved"
	}
	return m.popLayer(), nil
}

func (p *hookEditorPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m, w, h), w, h)
}

func (p *hookEditorPopup) box(m Model, w, h int) string {
	boxW := w * 8 / 10
	if boxW < 20 {
		boxW = w
	}
	// Size the scrollable script area so the WHOLE box fits within the terminal
	// height — otherwise overlayCenter gets a negative top offset and clips the
	// bottom help line off-screen. Chrome around the script: 5 content lines
	// (title, env, a blank, a blank, help) + modalStyle's frame (double border 2
	// + vertical padding 2 = 4) = 9, plus a 2-line margin so the box never touches
	// the screen edges.
	const chrome = 5 + 4
	rows := h - chrome - 2
	if rows < 3 {
		rows = 3
	}

	lines := strings.Split(p.buf.View(true), "\n")
	cur := strings.Count(string(p.buf.runes[:p.buf.cursor]), "\n") // cursor's line
	top := 0
	if cur >= rows {
		top = cur - rows + 1
	}
	end := top + rows
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	b.WriteString("Worktree post-create hook (runs in the new worktree)\n")
	b.WriteString("env: GG_MAIN_WORKTREE  GG_WORKTREE_PATH  GG_BRANCH  GG_REPO\n\n")
	b.WriteString(strings.Join(lines[top:end], "\n"))
	if end-top < rows {
		b.WriteString(strings.Repeat("\n", rows-(end-top)))
	}
	b.WriteString("\n\n[type] edit  [enter] newline  [ctrl+s] save  [esc] cancel")
	// No trailing newline: this box is sized to fill the height, so an extra
	// blank line would push overlayCenter's line count past the terminal.
	return modalStyle.Width(boxW).Render(b.String())
}
