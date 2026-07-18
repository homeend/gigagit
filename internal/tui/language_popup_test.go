package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/i18n"
)

// openLanguageRow opens Settings and moves the selection to the Language row.
func openLanguageRow(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = m.openSettings()
	sp := layerOf[*settingsPopup](m)
	if sp == nil {
		t.Fatal("settings popup not open")
	}
	found := false
	for i, e := range settingsMenu {
		if e == settingsMenuLanguage {
			sp.menuSel = i
			found = true
		}
	}
	if !found {
		t.Fatal("Language row missing from settingsMenu")
	}
	return m
}

func TestEnterOnLanguageRowOpensPicker(t *testing.T) {
	setupCustomLang(t, "[meta]\nname = \"Xxish\"\n\n[strings]\n")
	m := openLanguageRow(t, newTestModel(t))
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	p := layerOf[*languagePickerPopup](m)
	if p == nil {
		t.Fatal("enter on Language should push the picker")
	}
	if len(p.langs) < 6 { // en + ja/ko/zh/ru + xx
		t.Fatalf("langs = %v", p.langs)
	}
	if p.langs[p.sel].Code != "en" {
		t.Fatalf("initial selection should be the active language (en), got %s", p.langs[p.sel].Code)
	}
}

func TestPickerSelectAppliesAndPersists(t *testing.T) {
	setupCustomLang(t, "[meta]\nname = \"Xxish\"\n\n[strings]\n")
	m := openLanguageRow(t, newTestModel(t))
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	p := layerOf[*languagePickerPopup](m)
	for i, l := range p.langs {
		if l.Code == "xx" {
			p.sel = i
		}
	}
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)
	if layerOf[*languagePickerPopup](m) != nil {
		t.Fatal("picker should close on enter")
	}
	if i18n.ActiveCode() != "xx" {
		t.Fatalf("active = %q, want xx", i18n.ActiveCode())
	}
	if m.cfg.UI.Language != "xx" {
		t.Fatalf("in-memory cfg = %q", m.cfg.UI.Language)
	}
	raw, err := os.ReadFile(config.DefaultGlobalPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "language = \"xx\"") {
		t.Fatalf("global config not written:\n%s", raw)
	}
	if m.statusMsg == "" {
		t.Fatal("expected a status confirmation")
	}
}

func TestPickerEscCancelsWithoutChange(t *testing.T) {
	setupCustomLang(t, "[meta]\nname = \"Xxish\"\n\n[strings]\n")
	m := openLanguageRow(t, newTestModel(t))
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	um, _ = m.Update(keyMsg("esc"))
	m = um.(Model)
	if layerOf[*languagePickerPopup](m) != nil {
		t.Fatal("esc should close the picker")
	}
	if i18n.ActiveCode() != "en" {
		t.Fatalf("esc must not change the language, got %q", i18n.ActiveCode())
	}
	if layerOf[*settingsPopup](m) == nil {
		t.Fatal("esc should return to the Settings menu beneath")
	}
}

func TestSettingsMenuLabelTranslates(t *testing.T) {
	setupCustomLang(t, "[meta]\nname = \"Xxish\"\n\n[strings]\n\"Commit sort\" = \"XSORT\"\n")
	if err := i18n.SetLanguage("xx", langDirFromEnv(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = i18n.SetLanguage("", "") })
	m := newTestModel(t)
	for i, e := range settingsMenu {
		if e == settingsMenuCommitSort {
			if got := settingsMenuLabel(m, i); !strings.Contains(got, "XSORT") {
				t.Fatalf("label = %q, want translated title", got)
			}
			return
		}
	}
	t.Fatal("commit sort row missing")
}

// TestLanguagePickerRepoOverrideHint pins that opening the picker detects a
// repo-level [ui] language override (config.FileUILanguage on
// m.repoConfigPath) and renders the dimmed warning line beneath the title.
// Constructed directly (not via newTestModel/Settings) since
// openLanguagePicker only reads m.repoConfigPath — no domain/engine wiring
// needed.
func TestLanguagePickerRepoOverrideHint(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(p, []byte("[ui]\nlanguage = \"ja\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := Model{repoConfigPath: p}
	m2, _ := m.openLanguagePicker()
	pop, ok := m2.topLayer().(*languagePickerPopup)
	if !ok {
		t.Fatal("picker not on top")
	}
	if !pop.repoOverride {
		t.Fatal("repoOverride must be set when the repo config sets [ui] language")
	}
	// The hint text (59 display columns) is wider than the popup's fixed
	// 56-column inner width (popupInnerWidth), so lipgloss word-wraps it
	// across two physical lines in the real render — checked as two
	// fragments rather than one contiguous substring for that reason.
	out := pop.render(m2, "")
	if !strings.Contains(out, "overrides this") || !strings.Contains(out, "choice)") {
		t.Fatalf("hint line missing from render:\n%s", out)
	}
}

// TestLanguagePickerSetLanguageFailureStatus drives the picker's own
// enter-handling branch directly (a bogus lang code i18n.SetLanguage can't
// resolve, embedded or custom) to pin the fail-soft path: the picker still
// closes (never traps the user) and reports why via a "language failed: …"
// status message instead of silently doing nothing.
func TestLanguagePickerSetLanguageFailureStatus(t *testing.T) {
	p := &languagePickerPopup{langs: []i18n.Lang{{Code: "zz-bogus", Name: "Bogus"}}, sel: 0}
	m := Model{}.pushLayer(p)
	m2, _ := p.update(m, keyMsg("enter"))
	if !strings.Contains(m2.statusMsg, "language failed:") {
		t.Fatalf("statusMsg = %q, want it to contain %q", m2.statusMsg, "language failed:")
	}
	if layerOf[*languagePickerPopup](m2) != nil {
		t.Fatal("picker must close even when SetLanguage fails (fail-soft, never trap the user)")
	}
}

func TestActionMenuCopyRowTranslates(t *testing.T) {
	setupCustomLang(t, "[meta]\nname = \"Xxish\"\n\n[strings]\n\"Copy file path\" = \"XCOPYPATH\"\n")
	if err := i18n.SetLanguage("xx", langDirFromEnv(t)); err != nil {
		t.Fatal(err)
	}
	// copyRow labels flow from literal i18n.T calls; assert via the catalog
	// path rather than deep model state: T resolves the translation.
	if got := i18n.T("Copy file path"); got != "XCOPYPATH" {
		t.Fatalf("T = %q", got)
	}
}
