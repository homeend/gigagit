package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
)

func TestVersionsPolicyFromConfig(t *testing.T) {
	cases := []struct {
		name string
		in   config.VersionsConfig
		want engine.VersionsPolicy
	}{
		{
			name: "default 90-day retention, enabled",
			in:   config.VersionsConfig{Disabled: false, MaxAgeDays: 90},
			want: engine.VersionsPolicy{Enabled: true, MaxAgeDays: 90},
		},
		{
			name: "disabled",
			in:   config.VersionsConfig{Disabled: true, MaxAgeDays: 90},
			want: engine.VersionsPolicy{Enabled: false, MaxAgeDays: 90},
		},
		{
			name: "keep forever passes through",
			in:   config.VersionsConfig{Disabled: false, MaxAgeDays: -1},
			want: engine.VersionsPolicy{Enabled: true, MaxAgeDays: -1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Versions = tc.in
			got := versionsPolicyFromConfig(cfg)
			if got != tc.want {
				t.Fatalf("versionsPolicyFromConfig(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSaveVersionsRetentionWritesConfigAndPolicy(t *testing.T) {
	m, dir := settingsModel(t)
	m.repoConfigPath = filepath.Join(dir, ".gg.toml")

	m = m.saveVersionsRetention(-1)

	if m.cfg.Versions.MaxAgeDays != -1 {
		t.Fatalf("cfg.Versions.MaxAgeDays = %d, want -1", m.cfg.Versions.MaxAgeDays)
	}
	raw, err := os.ReadFile(m.repoConfigPath)
	if err != nil {
		t.Fatalf("saveVersionsRetention must write the repo .gg.toml: %v", err)
	}
	if !strings.Contains(string(raw), "max_age_days = -1") {
		t.Fatalf(".gg.toml missing max_age_days = -1:\n%s", raw)
	}
}

func TestSaveVersionsRetentionRejectsInvalidDays(t *testing.T) {
	m, dir := settingsModel(t)
	m.repoConfigPath = filepath.Join(dir, ".gg.toml")
	m.cfg.Versions.MaxAgeDays = 90

	m = m.saveVersionsRetention(0)
	if m.cfg.Versions.MaxAgeDays != 90 {
		t.Fatalf("days=0 must be rejected without changing cfg; got %d", m.cfg.Versions.MaxAgeDays)
	}
	if m.statusMsg == "" {
		t.Fatal("days=0 must surface a status message explaining the rejection")
	}
	if _, err := os.ReadFile(m.repoConfigPath); err == nil {
		t.Fatal("days=0 must not write the repo config")
	}

	m = m.saveVersionsRetention(-2)
	if m.cfg.Versions.MaxAgeDays != 90 {
		t.Fatalf("days=-2 must be rejected without changing cfg; got %d", m.cfg.Versions.MaxAgeDays)
	}
}

func TestToggleVersionsRecording(t *testing.T) {
	m, dir := settingsModel(t)
	m.repoConfigPath = filepath.Join(dir, ".gg.toml")
	if m.cfg.Versions.Disabled {
		t.Fatal("precondition: recording enabled")
	}

	m = m.toggleVersionsRecording()
	if !m.cfg.Versions.Disabled {
		t.Fatal("toggling must disable recording")
	}
	raw, err := os.ReadFile(m.repoConfigPath)
	if err != nil {
		t.Fatalf("toggleVersionsRecording must write the repo .gg.toml: %v", err)
	}
	if !strings.Contains(string(raw), "disabled = true") {
		t.Fatalf(".gg.toml missing disabled = true:\n%s", raw)
	}

	m = m.toggleVersionsRecording()
	if m.cfg.Versions.Disabled {
		t.Fatal("toggling back must re-enable recording")
	}
	raw, _ = os.ReadFile(m.repoConfigPath)
	if !strings.Contains(string(raw), "disabled = false") {
		t.Fatalf(".gg.toml missing disabled = false:\n%s", raw)
	}
}

// TestOpsHistMenuRowOpensSubView exercises the Settings (,) menu wiring: enter
// on "Operations history" opens the sub-view with both rows selectable, and
// esc backs out to the menu (mirrors the Refresh-rates sub-view contract).
func TestOpsHistMenuRowOpensSubView(t *testing.T) {
	m, dir := settingsModel(t)
	m.repoConfigPath = filepath.Join(dir, ".gg.toml")
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)

	p := layerOf[*settingsPopup](m)
	idx := -1
	for i, entry := range settingsMenu {
		if entry == settingsMenuOpsHist {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("Operations history entry missing from the settings menu")
	}
	p.menuSel = idx

	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	p = layerOf[*settingsPopup](m)
	if !p.opsHistView {
		t.Fatal("enter on Operations history must open the sub-view")
	}

	out := m.View()
	if !strings.Contains(out, "Retention") || !strings.Contains(out, "Recording") {
		t.Fatalf("sub-view must render both rows:\n%s", out)
	}

	// Recording row toggle: move down to row 1, enter toggles.
	u, _ = m.Update(keyMsg("down"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if !m.cfg.Versions.Disabled {
		t.Fatal("enter on the Recording row must toggle recording off")
	}

	// esc backs out to the menu (not editing, not another toggle).
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	p = layerOf[*settingsPopup](m)
	if p.opsHistView {
		t.Fatal("esc on the sub-view (not editing) must return to the menu")
	}
}

// TestOpsHistSubViewRendersAtWideWidth verifies that the Operations history
// sub-view renders at the wide popup width (popupWideInnerWidth) to avoid
// footer-hint overflow, especially with longer translations (e.g., Russian).
// This regression test ensures the width fix for opsHistView persists.
func TestOpsHistSubViewRendersAtWideWidth(t *testing.T) {
	m, dir := settingsModel(t)
	m.repoConfigPath = filepath.Join(dir, ".gg.toml")
	// Set terminal to a typical width (120 cols).
	m.width, m.height = 120, 50

	// Open Settings menu and navigate to Operations history.
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	p := layerOf[*settingsPopup](m)

	idx := -1
	for i, entry := range settingsMenu {
		if entry == settingsMenuOpsHist {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("Operations history entry missing from the settings menu")
	}
	p.menuSel = idx

	// Enter the sub-view.
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	p = layerOf[*settingsPopup](m)
	if !p.opsHistView {
		t.Fatal("enter on Operations history must open the sub-view")
	}

	// Render and verify the footer hint is not wrapped.
	out := m.View()

	// The footer hint when not editing: "[↑/↓] select  [enter] edit/toggle  [esc] back"
	// Verify it appears on a single line (not split across multiple lines).
	// Strip ANSI codes for the check since the output may contain styling.
	lines := strings.Split(out, "\n")
	hintLine := ""
	for _, line := range lines {
		stripped := stripANSI(line)
		if strings.Contains(stripped, "[↑/↓] select") && strings.Contains(stripped, "[esc] back") {
			hintLine = stripped
			break
		}
	}
	if hintLine == "" {
		t.Fatalf("footer hint line not found in rendered output:\n%s", out)
	}

	// The full hint must fit on one line (no wrapping). The minimum width needed
	// for this is approximately the length of the longest hint line.
	// Verify the entire hint is present without being split.
	expectedHint := "[↑/↓] select  [enter] edit/toggle  [esc] back"
	if !strings.Contains(hintLine, expectedHint) {
		t.Fatalf("footer hint must contain full text without wrapping.\ngot: %q\nwant: %q", hintLine, expectedHint)
	}
}

// stripANSI removes ANSI escape codes from a string for testing.
func stripANSI(s string) string {
	// Simple ANSI strip: remove sequences like \x1b[...m
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
		} else if inEscape && r == 'm' {
			inEscape = false
		} else if !inEscape {
			result.WriteRune(r)
		}
	}
	return result.String()
}
