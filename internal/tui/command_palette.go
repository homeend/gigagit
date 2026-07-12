package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// paletteCommand is one runnable entry in the command palette: a label, the
// direct key that also triggers it (shown right-aligned), and a run handler that
// performs the action (typically closing the palette and opening a popup).
type paletteCommand struct {
	label   string
	keyHint string
	run     func(Model) (Model, tea.Cmd)
}

// commandPalette is the generic command launcher (ctrl+p). It holds the palette
// entries (see paletteCommands) and grows by adding a paletteCommand.
type commandPalette struct {
	popupMax
	cmds []paletteCommand
	sel  int
}

// paletteCommands is the registry of palette entries, in display order.
func paletteCommands() []paletteCommand {
	// Entries are listed alphabetically by label.
	return []paletteCommand{
		{label: "Apply patch…", run: Model.openApplyPatchPopup},
		{label: "File blame", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathBlame) }},
		{label: "File history", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathHistory) }},
		{label: "Find", keyHint: "F", run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openFileFinder() }},
		{label: "Git config explorer", run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openGitConfigExplorer() }},
		{label: "Open repo", run: func(m Model) (Model, tea.Cmd) { return m.openRepoPathPopup() }},
		{label: "Open shell", keyHint: "ctrl+o", run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openSubshell() }},
		{label: "Run shell command…", run: func(m Model) (Model, tea.Cmd) { return m.openShellCmdPopup() }},
		{label: "Set up agent skills (using-gg)", run: func(m Model) (Model, tea.Cmd) {
			m = m.popLayer()
			m, cmd := m.openSettings()
			m = m.openAgentPicker()
			if sp := layerOf[*settingsPopup](m); sp != nil {
				sp.pickerFromPalette = true
			}
			return m, cmd
		}},
		{label: "Show commit", keyHint: "#", run: Model.openGotoCommitPopup},
	}
}

// openCommandPalette pushes the palette onto the layer stack.
func (m Model) openCommandPalette() (Model, tea.Cmd) {
	return m.pushLayer(&commandPalette{cmds: paletteCommands()}), nil
}

func (p *commandPalette) move(d int) {
	if len(p.cmds) == 0 {
		return
	}
	p.sel += d
	if p.sel < 0 {
		p.sel = 0
	}
	if p.sel >= len(p.cmds) {
		p.sel = len(p.cmds) - 1
	}
}

func (p *commandPalette) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc":
		return m.popLayer(), nil
	case "up", "k":
		p.move(-1)
	case "down", "j":
		p.move(+1)
	case "enter":
		if p.sel < 0 || p.sel >= len(p.cmds) {
			return m, nil
		}
		// Launch the command ON TOP of the palette (don't pop it): the palette is
		// the source, so esc out of the launched popup reveals it again. A command
		// that opens a terminal surface (e.g. the files view) unwinds the palette
		// itself — see resolvedGotoCommit.
		return p.cmds[p.sel].run(m)
	}
	return m, nil
}

func (p *commandPalette) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *commandPalette) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	textW := popupTextWidth(inner)
	parts := []string{"Commands", ""}
	for i, c := range p.cmds {
		prefix := "  "
		st := lipgloss.NewStyle()
		if i == p.sel {
			prefix, st = "> ", selectedRow
		}
		// "label … keyHint": pad the label out so the key hint sits at the right.
		row := prefix + c.label
		if c.keyHint != "" {
			gap := textW - lipgloss.Width(row) - lipgloss.Width(c.keyHint)
			if gap < 1 {
				gap = 1
			}
			row += strings.Repeat(" ", gap) + c.keyHint
		}
		parts = append(parts, st.Render(padRight(row, textW)))
	}
	parts = append(parts, "", "[enter] run  [esc] close")
	return popupBox(inner, strings.Join(parts, "\n"))
}
