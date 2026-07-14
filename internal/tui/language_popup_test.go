package tui

import (
	"os"
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
