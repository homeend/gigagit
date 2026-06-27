package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetGlobalDebugLogOperationsUncommentsTemplateLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(Template()), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetGlobalDebugLogOperations(path, true); err != nil {
		t.Fatalf("set: %v", err)
	}

	// Round-trips through Load: the value is now active.
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Debug.LogOperations {
		t.Fatal("expected log_operations=true after enabling")
	}

	// Comments and other sections survive (non-destructive edit).
	data, _ := os.ReadFile(path)
	body := string(data)
	if !strings.Contains(body, "[worktree]") || !strings.Contains(body, "[ui]") {
		t.Fatalf("other sections lost:\n%s", body)
	}
	if !strings.Contains(body, "# gg configuration") {
		t.Fatalf("header comment lost:\n%s", body)
	}

	// Toggling back off is also honored.
	if err := SetGlobalDebugLogOperations(path, false); err != nil {
		t.Fatalf("unset: %v", err)
	}
	cfg, _ = Load(path, "")
	if cfg.Debug.LogOperations {
		t.Fatal("expected log_operations=false after disabling")
	}
}

func TestSetGlobalDebugLogOperationsCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := SetGlobalDebugLogOperations(path, true); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Debug.LogOperations {
		t.Fatal("expected log_operations=true in freshly created file")
	}
}

func TestDebugOverlayRepoWins(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "config.toml")
	repo := filepath.Join(dir, ".gg.toml")
	os.WriteFile(global, []byte("[debug]\nlog_operations = true\n"), 0o644)
	os.WriteFile(repo, []byte(""), 0o644)

	cfg, err := Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	// Inverted polarity: an empty repo layer cannot reset the global's true.
	if !cfg.Debug.LogOperations {
		t.Fatal("global true should survive an empty repo layer")
	}
}

func TestSetGlobalRefreshDisableAdaptiveRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[refresh]\nenabled = true\nstatus = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetGlobalRefreshDisableAdaptive(path, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Refresh.DisableAdaptive {
		t.Fatal("disable_adaptive should be true after write")
	}
	// Other keys survive the line edit.
	if !cfg.Refresh.Enabled || cfg.Refresh.Status != 30 {
		t.Fatalf("unrelated keys clobbered: enabled=%v status=%d", cfg.Refresh.Enabled, cfg.Refresh.Status)
	}
	// Flip back to false (a default-on toggle must write both values).
	if err := SetGlobalRefreshDisableAdaptive(path, false); err != nil {
		t.Fatal(err)
	}
	cfg2, _ := Load(path, "")
	if cfg2.Refresh.DisableAdaptive {
		t.Fatal("disable_adaptive should be false after second write")
	}
}

func TestSetGlobalRefreshEnabledRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetGlobalRefreshEnabled(path, true); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, "")
	if err != nil || !c.Refresh.Enabled {
		t.Fatalf("enabled not persisted: %+v err=%v", c.Refresh, err)
	}
	if err := SetGlobalRefreshEnabled(path, false); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(path, "")
	if c.Refresh.Enabled {
		t.Fatal("disabled not persisted")
	}
}
