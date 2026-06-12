package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.Worktree.PathTemplate != "../<repo>.worktrees/<branch>" {
		t.Errorf("path default = %q", d.Worktree.PathTemplate)
	}
	if d.Worktree.DefaultBranchTemplate != "b/from-<parent-branch>-<random-alpha:4>" {
		t.Errorf("branch default = %q", d.Worktree.DefaultBranchTemplate)
	}
}

func TestDefaultGlobalPathXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := DefaultGlobalPath(); got != filepath.Join("/xdg", "gg", "config.toml") {
		t.Errorf("xdg path = %q", got)
	}
}

func TestDefaultGlobalPathHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got := DefaultGlobalPath(); got != filepath.Join("/home/u", ".config", "gg", "config.toml") {
		t.Errorf("home path = %q", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingFilesYieldDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "nope-global.toml"), filepath.Join(dir, "nope-repo.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Config has a slice field and is not comparable with ==; check the fields
	// that Defaults() populates.
	if cfg.Worktree.PathTemplate != Defaults().Worktree.PathTemplate {
		t.Errorf("missing files should yield default path, got %q", cfg.Worktree.PathTemplate)
	}
}

func TestLoadGlobalOnly(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "global.toml")
	writeFile(t, g, "[worktree]\npath_template = \"G/<branch>\"\n")
	cfg, err := Load(g, filepath.Join(dir, "missing-repo.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Worktree.PathTemplate != "G/<branch>" {
		t.Errorf("global path not applied: %q", cfg.Worktree.PathTemplate)
	}
	// Field the global did not set falls back to default.
	if cfg.Worktree.DefaultBranchTemplate != Defaults().Worktree.DefaultBranchTemplate {
		t.Errorf("unset field should keep default, got %q", cfg.Worktree.DefaultBranchTemplate)
	}
}

func TestLoadRepoWinsFieldLevel(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "global.toml")
	r := filepath.Join(dir, "repo.toml")
	// Global sets BOTH scalar fields; repo overrides only path_template.
	writeFile(t, g, "[worktree]\npath_template = \"G/<branch>\"\ndefault_branch_template = \"g-default\"\n")
	writeFile(t, r, "[worktree]\npath_template = \"R/<branch>\"\n")
	cfg, err := Load(g, r)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Worktree.PathTemplate != "R/<branch>" {
		t.Errorf("repo should win path_template, got %q", cfg.Worktree.PathTemplate)
	}
	// CRITICAL: repo setting one field must NOT wipe the global's other field.
	if cfg.Worktree.DefaultBranchTemplate != "g-default" {
		t.Errorf("global default_branch_template should survive, got %q", cfg.Worktree.DefaultBranchTemplate)
	}
}

func TestLoadRepoBranchTemplates(t *testing.T) {
	dir := t.TempDir()
	r := filepath.Join(dir, "repo.toml")
	writeFile(t, r, "[worktree]\nbranch_templates = [\"issue/<user:id>\", \"b/<parent-branch>\"]\n")
	cfg, err := Load(filepath.Join(dir, "missing.toml"), r)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Worktree.BranchTemplates) != 2 || cfg.Worktree.BranchTemplates[0] != "issue/<user:id>" {
		t.Errorf("branch_templates not loaded: %v", cfg.Worktree.BranchTemplates)
	}
}

func TestLoadMalformedTOMLErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.toml")
	writeFile(t, bad, "this is not = = valid toml [[[")
	if _, err := Load(bad, filepath.Join(dir, "missing.toml")); err == nil {
		t.Fatal("malformed global TOML should error")
	}
}

func TestUIWheelStepLayers(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")

	// Default.
	cfg, err := Load(missing, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.WheelStep != 3 {
		t.Errorf("default wheel_step = %d, want 3", cfg.UI.WheelStep)
	}

	// Global only.
	g := filepath.Join(dir, "global.toml")
	writeFile(t, g, "[ui]\nwheel_step = 5\n")
	cfg, err = Load(g, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.WheelStep != 5 {
		t.Errorf("global wheel_step = %d, want 5", cfg.UI.WheelStep)
	}

	// Repo wins over global.
	r := filepath.Join(dir, "repo.toml")
	writeFile(t, r, "[ui]\nwheel_step = 7\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.WheelStep != 7 {
		t.Errorf("repo wheel_step = %d, want 7", cfg.UI.WheelStep)
	}

	// Zero and negative are unset: the repo layer cannot reset the global's.
	writeFile(t, r, "[ui]\nwheel_step = -2\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.WheelStep != 5 {
		t.Errorf("negative wheel_step must be ignored, got %d, want global 5", cfg.UI.WheelStep)
	}
}
