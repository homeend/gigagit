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

func TestSetWorktreePostCreateHookRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	script := "cp \"$GG_MAIN_WORKTREE/.env\" .\nmake setup\n"
	if err := SetWorktreePostCreateHook(path, script); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Join(t.TempDir(), "no-global.toml"), path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worktree.PostCreateHook != script {
		t.Fatalf("round-trip = %q, want %q", cfg.Worktree.PostCreateHook, script)
	}
}

func TestSetWorktreePostCreateHookReplaceAndRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	if err := SetWorktreePostCreateHook(path, "echo one\n"); err != nil {
		t.Fatal(err)
	}
	if err := SetWorktreePostCreateHook(path, "echo two\n"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Count(string(raw), "post_create_hook") != 1 {
		t.Fatalf("expected exactly one hook block, got:\n%s", raw)
	}
	if !strings.Contains(string(raw), "echo two") || strings.Contains(string(raw), "echo one") {
		t.Fatalf("replace failed:\n%s", raw)
	}
	if err := SetWorktreePostCreateHook(path, ""); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "post_create_hook") {
		t.Fatalf("empty script must remove key:\n%s", raw)
	}
}

func TestCopyRepoConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.toml")
	dst := filepath.Join(dir, "sub", "b.toml") // parent dir must be created
	writeFile(t, src, "[ui]\ncommit_sort = \"plain\"\n")
	if err := CopyRepoConfig(src, dst); err != nil {
		t.Fatalf("CopyRepoConfig: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != "[ui]\ncommit_sort = \"plain\"\n" {
		t.Errorf("copy mismatch: %q", got)
	}
}

func TestCopyRepoConfigMissingSrc(t *testing.T) {
	dir := t.TempDir()
	if err := CopyRepoConfig(filepath.Join(dir, "nope.toml"), filepath.Join(dir, "b.toml")); err == nil {
		t.Error("expected error copying a missing source")
	}
}

func TestRemoveRepoConfigAbsentIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := RemoveRepoConfig(filepath.Join(dir, "nope.toml")); err != nil {
		t.Errorf("removing an absent file should be a no-op, got %v", err)
	}
}

func TestActiveRepoConfigPath(t *testing.T) {
	dir := t.TempDir()
	committed := filepath.Join(dir, ".gg.toml")
	private := filepath.Join(dir, "private.toml")
	writeFile(t, committed, "")
	// private absent → committed
	if got := ActiveRepoConfigPath(committed, private); got != committed {
		t.Errorf("private absent: want committed %q, got %q", committed, got)
	}
	// private present → private
	writeFile(t, private, "")
	if got := ActiveRepoConfigPath(committed, private); got != private {
		t.Errorf("private present: want private %q, got %q", private, got)
	}
	// empty private path → committed
	if got := ActiveRepoConfigPath(committed, ""); got != committed {
		t.Errorf("empty private: want committed %q, got %q", committed, got)
	}
}

func TestSetWorktreePostCreateHookIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	script := "cp a b\nmake\n"
	if err := SetWorktreePostCreateHook(path, script); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	cfg, _ := Load(filepath.Join(t.TempDir(), "ng.toml"), path)
	if err := SetWorktreePostCreateHook(path, cfg.Worktree.PostCreateHook); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatalf("re-save not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// Regression: a hook whose script contains lines that look like TOML structure
// ([ … ], key = value, # comment) must not corrupt a subsequent scalar write.
func TestScalarWriteSurvivesHookBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	script := "[ -d node_modules ] || npm ci\n# set up\nfoo = bar\n"
	if err := SetWorktreePostCreateHook(path, script); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := SetRefreshInterval(path, "branches", 30); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Join(t.TempDir(), "ng.toml"), path)
	if err != nil {
		t.Fatalf("Load after scalar write: %v", err)
	}
	if cfg.Worktree.PostCreateHook != script {
		t.Fatalf("hook corrupted by scalar write: %q", cfg.Worktree.PostCreateHook)
	}
	if cfg.Refresh.Branches != 30 {
		t.Fatalf("branches = %d, want 30", cfg.Refresh.Branches)
	}
	_ = before
}
