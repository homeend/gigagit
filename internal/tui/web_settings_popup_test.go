package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
)

func webMenuIndex(t *testing.T) int {
	t.Helper()
	for i, e := range settingsMenu {
		if e == settingsMenuWeb {
			return i
		}
	}
	t.Fatal("settingsMenuWeb is not in settingsMenu")
	return -1
}

func TestSettingsWebRowShowsState(t *testing.T) {
	m := loadedModel(t)
	if got := settingsMenuLabel(m, webMenuIndex(t)); !strings.Contains(got, "not running") {
		t.Fatalf("row = %q", got)
	}
	installFakeHost(t)
	m, cmd := m.openInBrowser()
	m = runOne(t, m, cmd)
	if got := settingsMenuLabel(m, webMenuIndex(t)); !strings.Contains(got, "http://127.0.0.1:4242") {
		t.Fatalf("row = %q", got)
	}
}

func TestWebSettingsPopupTogglesServeAndEditsAddr(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg"))
	global := config.DefaultGlobalPath()
	m := loadedModel(t)
	m = m.openWebSettings()
	p := layerOf[*webSettingsPopup](m)
	if p == nil {
		t.Fatal("popup pushed")
	}
	// Row 0 = Serve at startup: enter toggles and writes the global config.
	m, _ = p.update(m, keyMsg("enter"))
	if !m.cfg.Web.Serve {
		t.Fatal("serve toggled on")
	}
	if b, _ := os.ReadFile(global); !strings.Contains(string(b), "serve = true") {
		t.Fatalf("global config = %q", b)
	}
	// Row 1 = Address: enter opens the field, typing + enter saves.
	m, _ = p.update(m, keyMsg("down"))
	m, _ = p.update(m, keyMsg("enter"))
	if !p.editing {
		t.Fatal("enter on the address row edits it")
	}
	for _, r := range "127.0.0.1:7777" {
		m, _ = p.update(m, keyMsg(string(r)))
	}
	m, _ = p.update(m, keyMsg("enter"))
	if m.cfg.Web.Addr != "127.0.0.1:7777" {
		t.Fatalf("addr = %q", m.cfg.Web.Addr)
	}
	if b, _ := os.ReadFile(global); !strings.Contains(string(b), `addr = "127.0.0.1:7777"`) {
		t.Fatalf("global config = %q", b)
	}
	// The popup shows both.
	out := p.render(m, "")
	if !strings.Contains(out, "127.0.0.1:7777") || !strings.Contains(out, "on") {
		t.Fatalf("render = %q", out)
	}
	// esc closes back to Settings.
	m, _ = p.update(m, keyMsg("esc"))
	if layerOf[*webSettingsPopup](m) != nil {
		t.Fatal("esc pops the editor")
	}
}

func TestWebSettingsPopupOpenRowStartsThePage(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	m = m.openWebSettings()
	p := layerOf[*webSettingsPopup](m)
	m, _ = p.update(m, keyMsg("down"))
	m, _ = p.update(m, keyMsg("down"))
	m, cmd := p.update(m, keyMsg("enter"))
	m = runOne(t, m, cmd)
	if f.starts != 1 || f.opens != 1 {
		t.Fatalf("starts=%d opens=%d", f.starts, f.opens)
	}
}
