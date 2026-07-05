package tui

import (
	"os"
	"os/exec"
	"strings"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

// toolWizardRow is one detected tool × catalog command template pairing shown
// in the External-tools wizard.
type toolWizardRow struct {
	det      exttool.Detection
	tmpl     exttool.CommandTemplate
	existing bool // a (category,name) block already in config — shown, never rewritten
}

// openToolsWizard detects installed catalog tools and builds the wizard rows.
// New rows default checked (opening the wizard signals intent to add);
// existing rows show checked but are always skipped on apply.
func (m Model) openToolsWizard() Model {
	p := layerOf[*settingsPopup](m)
	have := map[string]bool{}
	for _, tc := range m.cfg.Tools.Command {
		have[tc.Key()] = true
	}
	p.toolRows = nil
	for _, det := range exttool.Detect(exec.LookPath, os.Stat) {
		for _, ct := range det.Tool.Commands {
			key := string(ct.Category) + "\x00" + ct.Name
			p.toolRows = append(p.toolRows, toolWizardRow{det: det, tmpl: ct, existing: have[key]})
		}
	}
	p.toolChecked = make([]bool, len(p.toolRows))
	for i := range p.toolChecked {
		p.toolChecked[i] = true
	}
	p.sel = 0
	p.toolsView = true
	return m
}

// applyToolsWizard appends the checked, not-yet-configured rows to the config
// file at globalPath and reloads the effective config. Returns the number of
// blocks written. globalPath is a parameter so tests never touch the real
// global config.
func (m Model) applyToolsWizard(rows []toolWizardRow, checked []bool, globalPath string) (Model, int, error) {
	var blocks []config.ToolCommand
	for i, row := range rows {
		if i >= len(checked) || !checked[i] || row.existing {
			continue
		}
		blocks = append(blocks, config.ToolCommand{
			Category: string(row.tmpl.Category),
			Name:     row.tmpl.Name,
			Mode:     string(row.tmpl.Mode),
			PerFile:  row.tmpl.PerFile,
			WhenOp:   row.tmpl.WhenOp,
			Command:  exttool.GenerateCommand(row.tmpl, row.det.Bin),
		})
	}
	if len(blocks) == 0 {
		return m, 0, nil
	}
	if err := config.AppendToolCommands(globalPath, blocks); err != nil {
		return m, 0, err
	}
	if cfg, err := config.Load(globalPath, m.repoConfigPath); err == nil {
		m.cfg = cfg
	}
	return m, len(blocks), nil
}

// toolConfiguredSuffixDecorator dims the trailing " (configured)" suffix on a
// row that already has a matching config block, using the same post-slice
// column-span technique as configRowDecorator (gitconfig_popup.go): text
// stays raw so width-based truncation/wrap never has to reason about an
// embedded escape sequence, and the color is applied to the final visible
// line at the exact rune columns [baseLen, baseLen+suffixLen). Only the
// row's first visual line is decorated (wrap continuations restart their
// rune index at 0 — the same caveat configRowDecorator documents).
func toolConfiguredSuffixDecorator(baseLen, suffixLen int) rowDecorator {
	return func(visible string, hscroll, visualLine int) string {
		if visualLine != 0 || suffixLen <= 0 {
			return visible
		}
		r := []rune(visible)
		var b strings.Builder
		i := 0
		for i < len(r) {
			col := i + hscroll
			if col >= baseLen && col < baseLen+suffixLen {
				j := i
				for j < len(r) {
					if c := j + hscroll; c < baseLen || c >= baseLen+suffixLen {
						break
					}
					j++
				}
				b.WriteString(dimRowStyle.Render(string(r[i:j])))
				i = j
				continue
			}
			b.WriteRune(r[i])
			i++
		}
		return b.String()
	}
}
