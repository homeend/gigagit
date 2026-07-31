package tui

import (
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"

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
// New rows default checked (opening the wizard signals intent to add) except
// OptIn templates, which start unchecked; existing rows show checked but are
// always skipped on apply.
func (m Model) openToolsWizard() Model {
	p := layerOf[*settingsPopup](m)
	have := map[string]bool{}
	for _, tc := range m.cfg.Tools.Command {
		have[tc.Key()] = true
	}
	p.toolRows = nil
	home, _ := os.UserHomeDir()
	for _, det := range exttool.Detect(exec.LookPath, os.Stat, home) {
		for _, ct := range det.Tool.Commands {
			key := string(ct.Category) + "\x00" + ct.Name
			p.toolRows = append(p.toolRows, toolWizardRow{det: det, tmpl: ct, existing: have[key]})
		}
	}
	p.toolChecked = defaultToolChecked(p.toolRows)
	p.sel = 0
	p.toolsView = true
	return m
}

// defaultToolChecked computes the wizard's initial checkbox states: a new
// row defaults checked, EXCEPT an OptIn template (an aggressive
// yolo/auto-approve variant) and EVERY conflict_complete row — that whole
// category completes the user's paused operation autonomously, so adding
// any of it is an explicit opt-in even where no bypass flag exists to mark
// OptIn (Kimi). An existing row stays checked as before — it is skipped on
// apply regardless.
func defaultToolChecked(rows []toolWizardRow) []bool {
	checked := make([]bool, len(rows))
	for i, row := range rows {
		aggressive := row.tmpl.OptIn || row.tmpl.Category == exttool.CatConflictComplete
		checked[i] = row.existing || !aggressive
	}
	return checked
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

// wrapWords greedily word-wraps s into lines of at most w display columns,
// breaking at spaces so a command's flags/tokens are never split mid-word —
// the live-feedback bug (wrapWidth's plain column-chunking turned
// "--allowedTools" into "--allowedToo" / "ls" across two lines). A word wider
// than w on its own can never fit any line, so it falls back to wrapWidth's
// hard column-chunking for that one word only. Whitespace runs collapse to a
// single space (strings.Fields), so a continuation line's leading indent is
// not preserved — acceptable for a dimmed preview, not the config write
// itself. An empty input yields no lines (mirrors wrapWidth on "").
func wrapWords(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var out []string
	cur := ""
	for _, word := range words {
		if lipgloss.Width(word) > w {
			// No space to break on: hard-chunk the oversized word itself, then
			// keep accumulating onto its last (short) chunk.
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			chunks := wrapWidth(word, w, 1<<20)
			if n := len(chunks); n > 0 {
				out = append(out, chunks[:n-1]...)
				cur = chunks[n-1]
			}
			continue
		}
		switch {
		case cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// dimSpanRunes returns the rune index range [from,to) of r whose display
// columns fall inside [startW, startW+width), given the row is already
// scrolled hscroll display columns. Columns, not rune indexes — a wide
// (CJK) rune advances the column by 2.
func dimSpanRunes(r []rune, hscroll, startW, width int) (int, int) {
	from, to := -1, -1
	col := hscroll
	for i, c := range r {
		w := lipgloss.Width(string(c))
		if col >= startW && col < startW+width {
			if from < 0 {
				from = i
			}
			to = i + 1
		}
		col += w
	}
	if from < 0 {
		return 0, 0
	}
	return from, to
}

// toolConfiguredSuffixDecorator dims the trailing " (configured)" suffix on a
// row that already has a matching config block, using the same post-slice
// column-span technique as configRowDecorator (gitconfig_popup.go): text
// stays raw so width-based truncation/wrap never has to reason about an
// embedded escape sequence, and the color is applied to the final visible
// line at the exact DISPLAY COLUMNS [baseW, baseW+suffixW) — not rune
// indexes, so a wide (CJK) rune in the base text doesn't throw off where the
// suffix span starts. Only the row's first visual line is decorated (wrap
// continuations restart their column count at 0 — the same caveat
// configRowDecorator documents).
func toolConfiguredSuffixDecorator(baseW, suffixW int) rowDecorator {
	return func(visible string, hscroll, visualLine int) string {
		if visualLine != 0 || suffixW <= 0 {
			return visible
		}
		r := []rune(visible)
		from, to := dimSpanRunes(r, hscroll, baseW, suffixW)
		if from == to {
			return visible
		}
		return string(r[:from]) + dimRowStyle.Render(string(r[from:to])) + string(r[to:])
	}
}
