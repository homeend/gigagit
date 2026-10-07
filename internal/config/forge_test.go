package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestForgeConfigDefaultsAndClamps(t *testing.T) {
	t.Parallel()
	var f ForgeConfig
	if f.CacheMaxAge() != 8*time.Hour || f.PrefetchCount() != 5 {
		t.Fatalf("defaults = %v, %d", f.CacheMaxAge(), f.PrefetchCount())
	}
	zero, big, neg := 0, 99, -3
	f = ForgeConfig{CacheHours: &zero, Prefetch: &zero}
	if f.CacheMaxAge() != 0 || f.PrefetchCount() != 0 {
		t.Fatalf("explicit 0 = %v, %d", f.CacheMaxAge(), f.PrefetchCount())
	}
	f = ForgeConfig{CacheHours: &neg, Prefetch: &big}
	if f.CacheMaxAge() != 8*time.Hour || f.PrefetchCount() != 50 {
		t.Fatalf("clamped = %v, %d", f.CacheMaxAge(), f.PrefetchCount())
	}
	f = ForgeConfig{Prefetch: &neg}
	if f.PrefetchCount() != 0 {
		t.Fatalf("negative prefetch = %d, want 0 (off)", f.PrefetchCount())
	}
}

// An explicit 0 in the repo layer overlays the global value; an unset layer
// leaves it alone; no layer = the defaults.
func TestLoadForgeLayers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	global, repo := filepath.Join(dir, "g.toml"), filepath.Join(dir, "r.toml")
	if err := os.WriteFile(global, []byte("[forge]\ncache_hours = 12\nprefetch = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repo, []byte("[forge]\ncache_hours = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Forge.CacheMaxAge() != 0 || cfg.Forge.PrefetchCount() != 2 {
		t.Fatalf("layered = %v, %d; want 0, 2", cfg.Forge.CacheMaxAge(), cfg.Forge.PrefetchCount())
	}
	cfg, err = Load(filepath.Join(dir, "none.toml"), filepath.Join(dir, "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Forge.CacheMaxAge() != 8*time.Hour || cfg.Forge.PrefetchCount() != 5 {
		t.Fatalf("no layer = %v, %d", cfg.Forge.CacheMaxAge(), cfg.Forge.PrefetchCount())
	}
}
