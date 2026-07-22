package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionsDefaultsAndOverlay(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	repo := filepath.Join(dir, "repo.toml")

	cfg, err := Load(global, repo) // neither file exists
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Versions.Disabled || cfg.Versions.MaxAgeDays != 90 {
		t.Fatalf("defaults = %+v, want enabled/90", cfg.Versions)
	}

	os.WriteFile(repo, []byte("[versions]\ndisabled = true\nmax_age_days = -1\n"), 0o644)
	cfg, err = Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Versions.Disabled || cfg.Versions.MaxAgeDays != -1 {
		t.Fatalf("overlay = %+v, want disabled/-1 (forever)", cfg.Versions)
	}

	// zero-is-unset: an explicit 0 must NOT override the 90 default.
	os.WriteFile(repo, []byte("[versions]\nmax_age_days = 0\n"), 0o644)
	cfg, _ = Load(global, repo)
	if cfg.Versions.MaxAgeDays != 90 {
		t.Fatalf("max_age_days=0 should stay default 90, got %d", cfg.Versions.MaxAgeDays)
	}
}

func TestVersionsWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	if err := SetVersionsMaxAgeDays(path, -1); err != nil {
		t.Fatal(err)
	}
	if err := SetVersionsDisabled(path, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	for _, want := range []string{"[versions]", "max_age_days = -1", "disabled = true"} {
		if !strings.Contains(s, want) {
			t.Fatalf("file missing %q:\n%s", want, s)
		}
	}
}
