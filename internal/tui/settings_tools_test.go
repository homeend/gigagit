package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

func wizardRows(existing map[string]bool) []toolWizardRow {
	var rows []toolWizardRow
	for _, tl := range exttool.Builtins() {
		for _, ct := range tl.Commands {
			rows = append(rows, toolWizardRow{
				det:      exttool.Detection{Tool: tl, Bin: tl.Bins[0]},
				tmpl:     ct,
				existing: existing[string(ct.Category)+"\x00"+ct.Name],
			})
		}
	}
	return rows
}

func TestApplyToolsWizardWritesMissingOnly(t *testing.T) {
	m := toolCfg() // empty tools config
	path := filepath.Join(t.TempDir(), "config.toml")
	rows := wizardRows(map[string]bool{"conflict\x00Claude": true}) // Claude pre-existing
	checked := make([]bool, len(rows))
	for i := range checked {
		checked[i] = true
	}
	m2, n, err := m.applyToolsWizard(rows, checked, path)
	if err != nil {
		t.Fatal(err)
	}
	wantWritten := len(rows) - 1 // all but the existing Claude row
	if n != wantWritten {
		t.Errorf("wrote %d rows, want %d", n, wantWritten)
	}
	cfg, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools.Command) != wantWritten {
		t.Errorf("config has %d commands, want %d", len(cfg.Tools.Command), wantWritten)
	}
	for _, tc := range cfg.Tools.Command {
		if tc.Name == "Claude" {
			t.Error("existing Claude block must be skipped, not rewritten")
		}
		if tc.Command == "" || tc.Category != "conflict" {
			t.Errorf("generated block malformed: %+v", tc)
		}
	}
	// The in-memory config was reloaded onto the model.
	if len(m2.cfg.Tools.Command) != wantWritten {
		t.Errorf("model cfg not refreshed: %d", len(m2.cfg.Tools.Command))
	}
	// Generated commands must not contain <bin>.
	for _, tc := range m2.cfg.Tools.Command {
		if contains := tc.Command; contains != "" && strings.Contains(contains, "<bin>") {
			t.Errorf("<bin> leaked into generated command: %q", tc.Command)
		}
	}
}

func TestApplyToolsWizardUncheckedSkipped(t *testing.T) {
	m := toolCfg()
	path := filepath.Join(t.TempDir(), "config.toml")
	rows := wizardRows(nil)
	checked := make([]bool, len(rows)) // all false
	_, n, err := m.applyToolsWizard(rows, checked, path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("wrote %d rows, want 0", n)
	}
}

// TestToolsWizardPreviewShowsDestinationAndCommand guards the live user
// feedback fix: the wizard must show what [enter] will actually write
// (destination file + generated command) for the SELECTED row, not just a
// bare "[x] Tool — conflict: Name" list.
func TestToolsWizardPreviewShowsDestinationAndCommand(t *testing.T) {
	// A short, deterministic global-config path so the destination line never
	// wraps across lines in the (narrow, fixed-width) wizard popup regardless
	// of the machine's real $HOME length.
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	fake := toolWizardRow{
		det: exttool.Detection{
			Tool: exttool.Tool{ID: "fake", Label: "Fake Tool", Bins: []string{"faketool"}},
			Bin:  "faketool",
		},
		tmpl: exttool.CommandTemplate{
			Category: exttool.CatConflict, Name: "Fake", Mode: exttool.ModeTerminal,
			Command: "<bin> --resolve",
		},
	}
	existingRow := fake
	existingRow.tmpl.Name = "FakeExisting"
	existingRow.existing = true

	m := toolCfg()
	p := &settingsPopup{
		toolsView:   true,
		toolRows:    []toolWizardRow{fake, existingRow},
		toolChecked: []bool{true, true},
		sel:         0,
	}

	out := p.box(m)
	wantDest := "writes to: " + config.DefaultGlobalPath()
	if !strings.Contains(out, wantDest) {
		t.Fatalf("expected destination line %q in:\n%s", wantDest, out)
	}
	wantCmd := exttool.GenerateCommand(fake.tmpl, fake.det.Bin) // "faketool --resolve"
	if !strings.Contains(out, wantCmd) {
		t.Fatalf("expected generated command %q in:\n%s", wantCmd, out)
	}

	// Selecting the existing row swaps the destination line for the
	// skipped-on-apply notice — an existing row is never rewritten.
	p.sel = 1
	out2 := p.box(m)
	if !strings.Contains(out2, "already configured — skipped on apply") {
		t.Fatalf("expected skipped-on-apply line for the existing row:\n%s", out2)
	}
	if strings.Contains(out2, wantDest) {
		t.Fatalf("existing row must not show a destination line:\n%s", out2)
	}
}

// TestToolsWizardPreviewCapsMultilineCommand guards the real-world case the
// live feedback named directly: the Claude catalog command is multi-line
// (backslash continuations + long flag lines), not the single-line synthetic
// command used above. wrapWidth alone treats "\n" as a zero-width rune and
// absorbs it into a segment rather than starting a new rendered line, so a
// naive single wrapWidth(cmd, textW, 8) call would let this command's many
// real lines blow past the 8-line cap and push the footer hint (and [esc]
// back, the user's only way out) off the bottom of the popup.
func TestToolsWizardPreviewCapsMultilineCommand(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	claude := exttool.Builtins()[0] // Claude Code: multi-line conflict command
	if claude.ID != "claude" {
		t.Fatalf("expected Builtins()[0] to be claude, got %q", claude.ID)
	}
	row := toolWizardRow{
		det:  exttool.Detection{Tool: claude, Bin: "claude"},
		tmpl: claude.Commands[0],
	}

	m := toolCfg()
	p := &settingsPopup{
		toolsView:   true,
		toolRows:    []toolWizardRow{row},
		toolChecked: []bool{true},
		sel:         0,
	}

	out := p.box(m)
	if !strings.Contains(out, "[esc] back") {
		t.Fatalf("footer hint (incl. [esc] back) must still be visible when the command preview is long:\n%s", out)
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("a command this long must show the overflow indicator:\n%s", out)
	}
	// out is the fully-framed popup (modalStyle's border + padding around each
	// content line, e.g. "║  writes to: ...                    ║"), not raw
	// content — strip the border/fill so line matching and blank-line
	// detection work on the actual text.
	contentOf := func(ln string) string {
		return strings.TrimSpace(strings.Trim(ln, "║╔╗╚╝═"))
	}
	lines := strings.Split(out, "\n")
	// At most 8 rendered lines belong to the command preview itself: count
	// lines strictly between the destination line and the footer hint's
	// blank-line separator.
	destIdx, hintIdx := -1, -1
	for i, ln := range lines {
		c := contentOf(ln)
		if strings.HasPrefix(c, "writes to: ") {
			destIdx = i
		}
		if strings.Contains(c, "[esc] back") {
			hintIdx = i
		}
	}
	if destIdx == -1 || hintIdx == -1 {
		t.Fatalf("could not locate destination/hint lines:\n%s", out)
	}
	// Walk back from the hint to the blank separator line to find where the
	// command preview block ends.
	blankIdx := hintIdx
	for blankIdx > destIdx && contentOf(lines[blankIdx]) != "" {
		blankIdx--
	}
	cmdLineCount := blankIdx - destIdx - 1
	if cmdLineCount > 8 {
		t.Fatalf("command preview rendered %d lines, want <= 8:\n%s", cmdLineCount, out)
	}
}
