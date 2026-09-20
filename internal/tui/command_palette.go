package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// paletteCommand is one runnable entry in the command palette: a label, the
// direct key that also triggers it (shown right-aligned), and a run handler that
// performs the action (typically closing the palette and opening a popup).
type paletteCommand struct {
	label   string
	keyHint string
	run     func(Model) (Model, tea.Cmd)
	// feature, when set, is the preflight feature id this entry needs. The
	// entry is filtered out of an open palette when that feature is disabled
	// in this repository — the registry itself stays static so it can be
	// enumerated without a Service.
	feature string
}

// commandPalette is the generic command launcher (ctrl+p). It holds the palette
// entries (see paletteCommands) and grows by adding a paletteCommand. Like the
// . action menu it is always in type-to-filter mode: printable keys extend
// query, arrows move, enter runs — sel indexes visible(), never cmds.
type commandPalette struct {
	popupMax
	cmds  []paletteCommand
	sel   int
	query string
}

// visible is cmds narrowed to the labels containing query, case-insensitively.
func (p *commandPalette) visible() []paletteCommand {
	if p.query == "" {
		return p.cmds
	}
	q := strings.ToLower(p.query)
	var out []paletteCommand
	for _, c := range p.cmds {
		if strings.Contains(strings.ToLower(c.label), q) {
			out = append(out, c)
		}
	}
	return out
}

// paletteCommands is the registry of palette entries, in display order.
func paletteCommands() []paletteCommand {
	// Entries are listed alphabetically by label.
	return []paletteCommand{
		{label: i18n.T("Apply patch…"), run: Model.openApplyPatchPopup},
		{label: i18n.T("Branch versions…"), run: Model.openVersionBranchList, feature: domain.FeatureVersions},
		{label: i18n.T("Browse remote branches"), run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openRemoteHeadsBrowser() }},
		{label: i18n.T("Compare with link…"), run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openLinkComparePopup() }},
		{label: i18n.T("File blame"), run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathBlame) }},
		{label: i18n.T("File history"), run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathHistory) }},
		{label: i18n.T("Find"), keyHint: "F", run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openFileFinder() }},
		{label: i18n.T("Git config explorer"), run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openGitConfigExplorer() }},
		{label: i18n.T("Open gg:// link…"), keyHint: "#", run: Model.openGotoCommitPopup},
		{label: i18n.T("Open repo"), run: func(m Model) (Model, tea.Cmd) { return m.openRepoPathPopup() }},
		{label: i18n.T("Open shell"), keyHint: "ctrl+o", run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openSubshell() }},
		{label: i18n.T("Run shell command…"), run: func(m Model) (Model, tea.Cmd) { return m.openShellCmdPopup() }},
		{label: i18n.T("Search pull requests…"), keyHint: "A", feature: domain.FeatureForge, run: func(m Model) (Model, tea.Cmd) {
			m = m.popLayer()
			m = m.activateTab(panelPRs)
			return m.openPRSearch()
		}},
		{label: i18n.T("Set up agent skills (using-gg)"), run: func(m Model) (Model, tea.Cmd) {
			m = m.popLayer()
			m, cmd := m.openSettings()
			m = m.openAgentPicker()
			if sp := layerOf[*settingsPopup](m); sp != nil {
				sp.pickerFromPalette = true
			}
			return m, cmd
		}},
		{label: i18n.T("Show commit"), keyHint: "#", run: Model.openGotoCommitPopup},
	}
}

// openCommandPalette pushes the palette onto the layer stack.
func (m Model) openCommandPalette() (Model, tea.Cmd) {
	return m.pushLayer(&commandPalette{cmds: m.availablePaletteCommands()}), nil
}

func (p *commandPalette) move(d int) {
	n := len(p.visible())
	if n == 0 {
		p.sel = 0
		return
	}
	p.sel += d
	if p.sel < 0 {
		p.sel = 0
	}
	if p.sel >= n {
		p.sel = n - 1
	}
}

func (p *commandPalette) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// Arrows/pages move the selection live while typing; every printable key
	// (j, k and q included) stays query text.
	if filterMotion(msg, p.move, popupFilterPage) {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		// First esc clears an active filter; esc with no filter closes.
		if p.query != "" {
			p.query, p.sel = "", 0
			return m, nil
		}
		return m.popLayer(), nil
	case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete:
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
		}
		p.sel = 0
	case tea.KeySpace:
		// Labels contain spaces; space extends the filter like any rune.
		p.query += " "
		p.sel = 0
	case tea.KeyRunes:
		p.query += string(msg.Runes)
		p.sel = 0
	case tea.KeyEnter:
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) {
			return m, nil
		}
		// Launch the command ON TOP of the palette (don't pop it): the palette is
		// the source, so esc out of the launched popup reveals it again. A command
		// that opens a terminal surface (e.g. the files view) unwinds the palette
		// itself — see resolvedGotoCommit.
		return vis[p.sel].run(m)
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
	header := i18n.T("Commands")
	if p.query != "" {
		header += "  " + p.query + "█"
	}
	parts := []string{header, ""}
	s := st()
	vis := p.visible()
	if len(vis) == 0 {
		parts = append(parts, padRight("  "+i18n.T("(no match)"), textW))
	}
	for i, c := range vis {
		prefix := "  "
		st := lipgloss.NewStyle()
		if i == p.sel {
			prefix, st = "> ", s.selectedRow
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
	// Wrap the hint so [esc] close survives on a narrow popup.
	hint := []string{i18n.T("type to filter"), i18n.T("[enter] run"), i18n.T("[esc] close")}
	parts = append(parts, "")
	parts = append(parts, wrapParts(hint, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// availablePaletteCommands is paletteCommands() minus every entry whose
// preflight feature is disabled in this repository. Filtering here (rather
// than in the registry) keeps paletteCommands() a pure, Service-free list.
func (m Model) availablePaletteCommands() []paletteCommand {
	all := paletteCommands()
	out := make([]paletteCommand, 0, len(all))
	for _, c := range all {
		if c.feature == domain.FeatureVersions && !m.versionsFeatureEnabled() {
			continue
		}
		if c.feature == domain.FeatureForge && !m.forgeShown {
			continue // no usable forge CLI: no pull-request UI at all
		}
		out = append(out, c)
	}
	return out
}
