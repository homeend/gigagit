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
