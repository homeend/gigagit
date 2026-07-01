package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUIShowGraphLayers(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")

	// Default (key missing): on.
	cfg, err := Load(missing, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.ShowGraph != "on" {
		t.Errorf("default show_graph = %q, want on", cfg.UI.ShowGraph)
	}

	// Repo sets off (the remembered "Show as list" preference).
	r := filepath.Join(dir, "repo.toml")
	writeFile(t, r, "[ui]\nshow_graph = \"off\"\n")
	cfg, err = Load(missing, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.ShowGraph != "off" {
		t.Errorf("repo show_graph = %q, want off", cfg.UI.ShowGraph)
	}

	// Repo wins over global — including turning the graph back ON over a
	// global off (the string form exists precisely so this works; a bool's
	// false would be zero-is-unset and ignored).
	g := filepath.Join(dir, "global.toml")
	writeFile(t, g, "[ui]\nshow_graph = \"off\"\n")
	writeFile(t, r, "[ui]\nshow_graph = \"on\"\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.ShowGraph != "on" {
		t.Errorf("repo-over-global show_graph = %q, want on", cfg.UI.ShowGraph)
	}

	// Empty string is unset: falls back to the default (on).
	writeFile(t, r, "[ui]\nshow_graph = \"\"\n")
	cfg, err = Load(missing, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.ShowGraph != "on" {
		t.Errorf("empty show_graph must be ignored, got %q, want default on", cfg.UI.ShowGraph)
	}
}

func TestSetShowGraphWritesRepoToml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")

	if err := SetShowGraph(path, "off"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "show_graph = \"off\"") {
		t.Fatalf("file missing show_graph assignment:\n%s", raw)
	}

	// Round-trips through Load and flips in place without duplicating the key.
	cfg, err := Load(filepath.Join(dir, "missing.toml"), path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.ShowGraph != "off" {
		t.Fatalf("show_graph = %q after write, want off", cfg.UI.ShowGraph)
	}
	if err := SetShowGraph(path, "on"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Count(string(raw), "show_graph") != 1 {
		t.Fatalf("re-set duplicated the key:\n%s", raw)
	}
	cfg, err = Load(filepath.Join(dir, "missing.toml"), path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.ShowGraph != "on" {
		t.Fatalf("show_graph = %q after re-write, want on", cfg.UI.ShowGraph)
	}
}
