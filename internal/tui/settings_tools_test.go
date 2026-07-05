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
// naive single wrapWidth(cmd, textW, hardcodedCap) call would let this
// command's many real lines blow past a fixed cap and push the footer hint
// (and [esc] back, the user's only way out) off the bottom of the popup.
// Since the full-screen rewrite the cap is HEIGHT-derived rather than a
// hardcoded constant, so this test's job is no longer "count lines <= 8" but
// "the box never renders more lines than the terminal can show" — the actual
// invariant that keeps the footer from being clipped by overlayCenter (which
// silently drops rows past termH).
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
	// The height-budgeted preview cap exists precisely so the box (border +
	// padding + every content line) never exceeds the terminal height:
	// overlayCenter composites by centering on termH and silently drops any
	// fg row that falls outside it, which would clip the footer with no
	// visible sign anything was cut.
	_, termH := m.overlayDims()
	lines := strings.Split(out, "\n")
	if len(lines) > termH {
		t.Fatalf("box rendered %d lines, want <= terminal height %d (would clip the footer):\n%s", len(lines), termH, out)
	}
}

// TestToolsWizardWideTerminalAvoidsMidWordWrap guards the full-screen sizing
// itself: on a wide/tall terminal the wizard must actually USE that width
// (popupFullInnerWidth is uncapped, unlike the old fixed-56-column popup) so
// a long flag line renders on ONE line instead of wrapping — let alone
// wrapping mid-word, the exact live-feedback symptom ("--allowedToo" / "ls").
func TestToolsWizardWideTerminalAvoidsMidWordWrap(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	claude := exttool.Builtins()[0]
	row := toolWizardRow{det: exttool.Detection{Tool: claude, Bin: "claude"}, tmpl: claude.Commands[0]}

	m := toolCfg()
	m.width, m.height = 200, 50 // a wide/tall terminal the old 56-col popup ignored
	p := &settingsPopup{
		toolsView:   true,
		toolRows:    []toolWizardRow{row},
		toolChecked: []bool{true},
		sel:         0,
	}

	out := p.box(m)

	cmd := exttool.GenerateCommand(row.tmpl, row.det.Bin)
	var wantLine string
	for _, ln := range strings.Split(cmd, "\n") {
		if strings.Contains(ln, "--allowedTools") {
			wantLine = strings.TrimSpace(ln) // wrapWords collapses leading indent
			break
		}
	}
	if wantLine == "" {
		t.Fatal("could not find the --allowedTools line in the generated command")
	}
	if !strings.Contains(out, wantLine) {
		t.Fatalf("expected the --allowedTools line to render on one unwrapped line at full width:\nwant substring: %q\ngot:\n%s", wantLine, out)
	}
}
