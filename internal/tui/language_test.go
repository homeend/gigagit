package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/i18n"
)

// setupCustomLang points XDG_CONFIG_HOME at a temp dir holding a custom
// "xx" bundle and registers the English reset.
func setupCustomLang(t *testing.T, body string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	langDir := filepath.Join(tmp, "gg", "lang")
	if err := os.MkdirAll(langDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(langDir, "xx.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = i18n.SetLanguage("", "") })
}

func TestConfigReadyAppliesLanguage(t *testing.T) {
	setupCustomLang(t, "[meta]\nname = \"Xxish\"\n\n[strings]\n")
	m := newTestModel(t)
	cfg := config.Defaults()
	cfg.UI.Language = "xx"
	um, _ := m.Update(configReadyMsg{cfg: cfg})
	_ = um.(Model)
	if i18n.ActiveCode() != "xx" {
		t.Fatalf("active = %q, want xx", i18n.ActiveCode())
	}
}

func TestConfigReadyUnknownLanguageFailsSoft(t *testing.T) {
	setupCustomLang(t, "[meta]\nname = \"Xxish\"\n\n[strings]\n")
	m := newTestModel(t)
	cfg := config.Defaults()
	cfg.UI.Language = "nope"
	um, _ := m.Update(configReadyMsg{cfg: cfg})
	m = um.(Model)
	if i18n.ActiveCode() != "en" {
		t.Fatalf("active = %q, want en fallback", i18n.ActiveCode())
	}
	if !strings.Contains(m.statusMsg, "nope") {
		t.Fatalf("statusMsg = %q, want a notice naming the bad code", m.statusMsg)
	}
}

func TestDataLoadedAppliesLanguage(t *testing.T) {
	setupCustomLang(t, "[meta]\nname = \"Xxish\"\n\n[strings]\n")
	m := newTestModel(t)
	// drive the legacy load path (reRoot / repo switch)
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	cfg := m.cfg
	cfg.UI.Language = "xx"
	msg := dataLoadedMsg{gen: m.loadGen, cfg: cfg}
	um, _ := m.Update(msg)
	_ = um.(Model)
	if i18n.ActiveCode() != "xx" {
		t.Fatalf("active = %q, want xx", i18n.ActiveCode())
	}
}
