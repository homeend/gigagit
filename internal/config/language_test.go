package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUILanguageLayers(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	repo := filepath.Join(dir, ".gg.toml")

	// unset everywhere → empty (English)
	c, err := Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	if c.UI.Language != "" {
		t.Fatalf("default language = %q, want empty", c.UI.Language)
	}

	// global sets it
	os.WriteFile(global, []byte("[ui]\nlanguage = \"ja\"\n"), 0o644)
	c, _ = Load(global, repo)
	if c.UI.Language != "ja" {
		t.Fatalf("global layer = %q, want ja", c.UI.Language)
	}

	// repo overrides
	os.WriteFile(repo, []byte("[ui]\nlanguage = \"ru\"\n"), 0o644)
	c, _ = Load(global, repo)
	if c.UI.Language != "ru" {
		t.Fatalf("repo layer = %q, want ru", c.UI.Language)
	}
}

func TestSetGlobalUILanguageRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	// seed an unrelated key that must survive the edit
	os.WriteFile(path, []byte("[ui]\nshow_graph = \"off\"\n"), 0o644)
	if err := SetGlobalUILanguage(path, "ko"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, "")
	if err != nil || c.UI.Language != "ko" {
		t.Fatalf("language not persisted: %+v err=%v", c.UI, err)
	}
	if c.UI.ShowGraph != "off" {
		t.Fatal("unrelated ui key clobbered")
	}
	raw, _ := os.ReadFile(path)
	if strings.Count(string(raw), "language") != 1 {
		t.Fatalf("duplicate key written:\n%s", raw)
	}
}

func TestLangDirUnderConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	if got := LangDir(); got != filepath.Join("/tmp/xdg-test", "gg", "lang") {
		t.Fatalf("LangDir = %q", got)
	}
}
