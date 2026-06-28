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

func TestSetRefreshIntervalRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	if err := os.WriteFile(path, []byte("[refresh]\nenabled = true\nstatus = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetRefreshInterval(path, "branches", 45); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("", path) // repo layer
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Refresh.Branches != 45 {
		t.Fatalf("branches should be 45, got %d", cfg.Refresh.Branches)
	}
	// Unrelated keys survive.
	if !cfg.Refresh.Enabled || cfg.Refresh.Status != 30 {
		t.Fatalf("unrelated keys clobbered: enabled=%v status=%d", cfg.Refresh.Enabled, cfg.Refresh.Status)
	}
	// Update an existing key in place.
	if err := SetRefreshInterval(path, "status", 0); err != nil {
		t.Fatal(err)
	}
	cfg2, _ := Load("", path)
	if cfg2.Refresh.Status != 0 {
		t.Fatalf("status should be 0, got %d", cfg2.Refresh.Status)
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

func TestSetGlobalDisableRemoteTagsAutoRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	// Write an unrelated key first so we can verify it survives.
	if err := os.WriteFile(path, []byte("[refresh]\nenabled = true\nstatus = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write true → reload → DisableRemoteTagsAuto should be true.
	if err := SetGlobalDisableRemoteTagsAuto(path, true); err != nil {
		t.Fatalf("set true: %v", err)
	}
	c, err := Load(path, "")
	if err != nil {
		t.Fatalf("load after set true: %v", err)
	}
	if !c.Refresh.DisableRemoteTagsAuto {
		t.Fatal("DisableRemoteTagsAuto=true not persisted")
	}

	// Unrelated keys must survive the edit.
	if !c.Refresh.Enabled || c.Refresh.Status != 30 {
		t.Fatalf("unrelated keys clobbered: enabled=%v status=%d", c.Refresh.Enabled, c.Refresh.Status)
	}

	// Write false → reload → DisableRemoteTagsAuto should be false.
	if err := SetGlobalDisableRemoteTagsAuto(path, false); err != nil {
		t.Fatalf("set false: %v", err)
	}
	c, _ = Load(path, "")
	if c.Refresh.DisableRemoteTagsAuto {
		t.Fatal("DisableRemoteTagsAuto=false not persisted")
	}
}

func TestSetRefreshWatchRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	if err := SetRefreshWatch(path, "worktrees", true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("", path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Refresh.WorktreesWatch {
		t.Fatal("worktrees_watch did not round-trip to true")
	}
}

func TestSetRefreshWatchPreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	if err := SetRefreshInterval(path, "worktrees", 30); err != nil {
		t.Fatal(err)
	}
	if err := SetRefreshWatch(path, "worktrees", true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("", path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Refresh.Worktrees != 30 || !cfg.Refresh.WorktreesWatch {
		t.Fatalf("interval=%d watch=%v; want 30/true", cfg.Refresh.Worktrees, cfg.Refresh.WorktreesWatch)
	}
}
