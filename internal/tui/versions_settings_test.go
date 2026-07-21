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

// TestOpsHistSubViewRendersAtWideWidth verifies that opsHistView and ratesView
// both render at the same (wide) popup width. This regression test catches
// removal of p.opsHistView from the wide-box condition by rendering both
// sub-views and comparing their content line lengths: ratesView already uses
// wide width, so both should have identical line widths; if opsHistView loses
// the wide-width fix, its content lines will be noticeably shorter.
func TestOpsHistSubViewRendersAtWideWidth(t *testing.T) {
	m, dir := settingsModel(t)
	m.repoConfigPath = filepath.Join(dir, ".gg.toml")
	// Use a terminal width where the difference between popupInnerWidth (56)
	// and popupWideInnerWidth (96) is clear and in play: 100 cols.
	// At 100: narrow inner = min(56, 92) = 56, wide = min(96, 92) = 92.
	const termWidth = 100
	m.width, m.height = termWidth, 50

	// Helper: find the maximum content line length (excluding border/padding) from rendered output.
	// Content lines are prefixed with "> " (selection marker) or "  " (normal).
	maxContentLineLen := func(out string) int {
		maxLen := 0
		for _, line := range strings.Split(out, "\n") {
			stripped := stripANSI(line)
			// Skip empty lines and border lines (contain box-drawing chars or are borders)
			if len(stripped) == 0 || strings.ContainsAny(stripped, "╭╮╰╯─│") {
				continue
			}
			// Skip lines that are just padding
			if strings.TrimSpace(stripped) == "" {
				continue
			}
			// This is a content line; measure it
			if len(stripped) > maxLen {
				maxLen = len(stripped)
			}
		}
		return maxLen
	}

	// Open Settings menu.
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	p := layerOf[*settingsPopup](m)

	// Find and open Refresh rates (known to use wide width).
	ratesIdx := -1
	for i, entry := range settingsMenu {
		if entry == settingsMenuRates {
			ratesIdx = i
		}
	}
	if ratesIdx < 0 {
		t.Fatal("Refresh rates entry missing from the settings menu")
	}
	p.menuSel = ratesIdx

	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if !layerOf[*settingsPopup](m).ratesView {
		t.Fatal("failed to open Refresh rates sub-view")
	}

	ratesOut := m.View()
	ratesLineLen := maxContentLineLen(ratesOut)
	if ratesLineLen == 0 {
		t.Fatalf("could not measure Refresh rates content line length:\n%s", ratesOut)
	}

	// Close rates view to return to menu.
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	p = layerOf[*settingsPopup](m)
	if p.ratesView {
		t.Fatal("esc should close Refresh rates")
	}

	// Find and open Operations history.
	opsIdx := -1
	for i, entry := range settingsMenu {
		if entry == settingsMenuOpsHist {
			opsIdx = i
		}
	}
	if opsIdx < 0 {
		t.Fatal("Operations history entry missing from the settings menu")
	}
	p.menuSel = opsIdx

	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	p = layerOf[*settingsPopup](m)
	if !p.opsHistView {
		t.Fatal("failed to open Operations history sub-view")
	}

	opsOut := m.View()
	opsLineLen := maxContentLineLen(opsOut)
	if opsLineLen == 0 {
		t.Fatalf("could not measure Operations history content line length:\n%s", opsOut)
	}

	// Both views should render at the same width (wide width).
	// If opsHistView is using narrow width (missing fix), opsLineLen << ratesLineLen.
	// Allow small difference (±3 chars) for rendering variation, but not a major divergence.
	diff := ratesLineLen - opsLineLen
	if diff > 5 || diff < -5 {
		t.Fatalf("Operations history and Refresh rates content line widths differ significantly.\n"+
			"Refresh rates max content line: %d chars\n"+
			"Operations history max content line: %d chars\n"+
			"Difference: %d chars\n"+
			"Both should be ~%d (wide width). Missing fix? Check settings_popup.go line ~695.\n\n"+
			"Refresh rates output:\n%s\n\n"+
			"Operations history output:\n%s",
			ratesLineLen, opsLineLen, diff, ratesLineLen,
			ratesOut, opsOut)
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
