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

func TestUICommitSortLayers(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")

	// Default (key missing): date-order.
	cfg, err := Load(missing, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.CommitSort != "date-order" {
		t.Errorf("default commit_sort = %q, want date-order", cfg.UI.CommitSort)
	}

	// Repo sets plain (opt down on a big repo).
	r := filepath.Join(dir, "repo.toml")
	writeFile(t, r, "[ui]\ncommit_sort = \"plain\"\n")
	cfg, err = Load(missing, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.CommitSort != "plain" {
		t.Errorf("repo commit_sort = %q, want plain", cfg.UI.CommitSort)
	}

	// Repo wins over global.
	g := filepath.Join(dir, "global.toml")
	writeFile(t, g, "[ui]\ncommit_sort = \"date-order\"\n")
	writeFile(t, r, "[ui]\ncommit_sort = \"plain\"\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.CommitSort != "plain" {
		t.Errorf("repo-over-global commit_sort = %q, want plain", cfg.UI.CommitSort)
	}

	// Empty string is unset: cannot reset a lower layer to empty (falls back to default).
	writeFile(t, r, "[ui]\ncommit_sort = \"\"\n")
	cfg, err = Load(missing, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.CommitSort != "date-order" {
		t.Errorf("empty commit_sort must be ignored, got %q, want default date-order", cfg.UI.CommitSort)
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

	// Repo only (no global file).
	cfg, err = Load(missing, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.WheelStep != 7 {
		t.Errorf("repo-only wheel_step = %d, want 7", cfg.UI.WheelStep)
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

func TestHScrollStepDefaultAndOverlay(t *testing.T) {
	if got := Defaults().UI.HScrollStep; got != 8 {
		t.Fatalf("default hscroll_step = %d, want 8", got)
	}
	dst := Defaults().UI
	overlayUI(&dst, UIConfig{HScrollStep: 12})
	if dst.HScrollStep != 12 {
		t.Fatalf("overlay set hscroll_step = %d, want 12", dst.HScrollStep)
	}
	overlayUI(&dst, UIConfig{HScrollStep: 0}) // <=0 is unset, must not clobber
	if dst.HScrollStep != 12 {
		t.Fatalf("unset (0) must not overwrite; got %d", dst.HScrollStep)
	}
}

func TestOverlaySearchHistorySize(t *testing.T) {
	dst := UIConfig{SearchHistorySize: 0}
	overlayUI(&dst, UIConfig{SearchHistorySize: 50})
	if dst.SearchHistorySize != 50 {
		t.Fatalf("SearchHistorySize = %d, want 50", dst.SearchHistorySize)
	}
	// <= 0 in src must not reset a set dst (unset rule).
	overlayUI(&dst, UIConfig{SearchHistorySize: 0})
	if dst.SearchHistorySize != 50 {
		t.Fatalf("zero src must not reset, got %d", dst.SearchHistorySize)
	}
}

func TestOverlayReflogLimit(t *testing.T) {
	dst := UIConfig{ReflogLimit: 0}
	overlayUI(&dst, UIConfig{ReflogLimit: 42})
	if dst.ReflogLimit != 42 {
		t.Fatalf("ReflogLimit = %d, want 42", dst.ReflogLimit)
	}
	// <= 0 in src must not reset a set dst (unset rule).
	overlayUI(&dst, UIConfig{ReflogLimit: 0})
	if dst.ReflogLimit != 42 {
		t.Fatalf("zero src must not reset, got %d", dst.ReflogLimit)
	}
}

func TestOverlayUIActionLists(t *testing.T) {
	dst := Defaults().UI
	overlayUI(&dst, UIConfig{FooterActions: []string{"pull", "commit"}, MenuActions: []string{"pull"}})
	if len(dst.FooterActions) != 2 || dst.FooterActions[0] != "pull" {
		t.Fatalf("FooterActions = %v, want [pull commit]", dst.FooterActions)
	}
	// A non-empty list overrides; an empty/nil list is unset and must not clobber.
	overlayUI(&dst, UIConfig{FooterActions: []string{"push"}})
	if len(dst.FooterActions) != 1 || dst.FooterActions[0] != "push" {
		t.Fatalf("non-empty list must override; got %v", dst.FooterActions)
	}
	overlayUI(&dst, UIConfig{}) // empty lists = unset
	if len(dst.FooterActions) != 1 || len(dst.MenuActions) != 1 {
		t.Fatalf("empty lists must not clobber; got footer=%v menu=%v", dst.FooterActions, dst.MenuActions)
	}
}

func TestUIDefaultsCommitGraph(t *testing.T) {
	d := Defaults().UI
	if d.CommitGraphLanes != 8 || d.CommitGraphMinLanes != 2 || d.CommitGraphStep != 4 {
		t.Fatalf("defaults = %+v, want lanes 8 / min 2 / step 4", d)
	}
}

func TestLoadOverlaysCommitGraphFields(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo.toml")
	if err := os.WriteFile(repo, []byte("[ui]\ncommit_graph_lanes = 20\ncommit_graph_step = 6\ncommit_graph_max_lanes = 100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("", repo)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.CommitGraphLanes != 20 {
		t.Errorf("lanes = %d, want 20 (repo overrides default)", cfg.UI.CommitGraphLanes)
	}
	if cfg.UI.CommitGraphStep != 6 {
		t.Errorf("step = %d, want 6", cfg.UI.CommitGraphStep)
	}
	if cfg.UI.CommitGraphMaxLanes != 100 {
		t.Errorf("max = %d, want 100", cfg.UI.CommitGraphMaxLanes)
	}
	if cfg.UI.CommitGraphMinLanes != 2 {
		t.Errorf("min = %d, want default 2 (unset field keeps default)", cfg.UI.CommitGraphMinLanes)
	}
}

func TestOverlayDisableSlowOpConfirm(t *testing.T) {
	// Default zero value: confirmation enabled (field false).
	var def UIConfig
	if def.DisableSlowOpConfirm {
		t.Fatal("zero UIConfig should leave slow-op confirm enabled (DisableSlowOpConfirm=false)")
	}
	// A true in a higher layer overlays up to disable.
	dst := UIConfig{}
	overlayUI(&dst, UIConfig{DisableSlowOpConfirm: true})
	if !dst.DisableSlowOpConfirm {
		t.Fatal("overlayUI did not propagate DisableSlowOpConfirm=true")
	}
	// A false in a higher layer does NOT clear a true already set (OR-only).
	dst2 := UIConfig{DisableSlowOpConfirm: true}
	overlayUI(&dst2, UIConfig{DisableSlowOpConfirm: false})
	if !dst2.DisableSlowOpConfirm {
		t.Fatal("overlayUI must not clear an existing true (OR-only semantics)")
	}
}

func TestCommitPageSizeDefaultsAndOverlay(t *testing.T) {
	// Defaults.
	d := Defaults().UI
	if d.CommitInitialCount != 300 || d.CommitBatchSize != 300 || d.CommitSearchMaxPages != 5 {
		t.Fatalf("defaults = %d/%d/%d, want 300/300/5",
			d.CommitInitialCount, d.CommitBatchSize, d.CommitSearchMaxPages)
	}
	// Repo file overrides; a 0 in a higher layer does NOT reset a lower layer.
	dir := t.TempDir()
	repo := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(repo, []byte("[ui]\ncommit_initial_count = 25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Join(dir, "no-global.toml"), repo)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.CommitInitialCount != 25 {
		t.Fatalf("initial = %d, want 25 (repo override)", cfg.UI.CommitInitialCount)
	}
	if cfg.UI.CommitBatchSize != 300 {
		t.Fatalf("batch = %d, want 300 (default kept)", cfg.UI.CommitBatchSize)
	}
}

func TestOverlayRefreshMinSeconds(t *testing.T) {
	dst := RefreshConfig{}
	overlayRefresh(&dst, RefreshConfig{MinSeconds: 20})
	if dst.MinSeconds != 20 {
		t.Fatalf("MinSeconds should overlay, got %d", dst.MinSeconds)
	}
	overlayRefresh(&dst, RefreshConfig{MinSeconds: 0}) // zero-is-unset
	if dst.MinSeconds != 20 {
		t.Fatalf("zero must not reset, got %d", dst.MinSeconds)
	}
}

func TestRefreshConfigDefaultsOff(t *testing.T) {
	c := Defaults()
	if c.Refresh.Enabled {
		t.Error("refresh must default disabled")
	}
	if c.Refresh.Status != 0 || c.Refresh.Fetch != 0 {
		t.Error("refresh intervals must default 0 (off)")
	}
}

func TestOverlayRefreshRemoteTags(t *testing.T) {
	dst := RefreshConfig{}
	overlayRefresh(&dst, RefreshConfig{RemoteTags: 30})
	if dst.RemoteTags != 30 {
		t.Fatalf("RemoteTags should overlay, got %d", dst.RemoteTags)
	}
	overlayRefresh(&dst, RefreshConfig{RemoteTags: 0}) // zero-is-unset
	if dst.RemoteTags != 30 {
		t.Fatalf("zero must not reset, got %d", dst.RemoteTags)
	}
}

func TestRefreshConfigOverlayAndParse(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(repo, []byte("[refresh]\nenabled = true\nstatus = 30\nfetch = 300\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load("", repo)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Refresh.Enabled || c.Refresh.Status != 30 || c.Refresh.Fetch != 300 {
		t.Fatalf("parsed/overlaid refresh wrong: %+v", c.Refresh)
	}
}

// TestDisableRemoteTagsAutoDefault asserts the zero/default value is false
// (auto-refresh ON — inverted polarity).
func TestDisableRemoteTagsAutoDefault(t *testing.T) {
	if Defaults().Refresh.DisableRemoteTagsAuto {
		t.Fatal("default must be false (auto remote-tag refresh on)")
	}
}

// TestOverlayRefreshDisableRemoteTagsAutoInverted tests the inverted-polarity
// overlay: a global file with the key set to true propagates, and a missing
// key in the repo file does NOT reset it back to false.
func TestOverlayRefreshDisableRemoteTagsAutoInverted(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	missing := filepath.Join(dir, "missing.toml")

	// default: false (auto on)
	cfg, err := Load(missing, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Refresh.DisableRemoteTagsAuto {
		t.Fatal("default must be false (auto remote-tag refresh on)")
	}

	// global file with disable_remote_tags_auto = true → merged config has it true
	writeFile(t, global, "[refresh]\ndisable_remote_tags_auto = true\n")
	cfg, err = Load(global, missing)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Refresh.DisableRemoteTagsAuto {
		t.Fatal("global true should propagate to merged config")
	}

	// repo file with the key absent does NOT reset a true global back to false
	repo := filepath.Join(dir, "repo.toml")
	writeFile(t, repo, "[refresh]\nstatus = 30\n")
	cfg, err = Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Refresh.DisableRemoteTagsAuto {
		t.Fatal("absent repo key must not reset a true global back to false (OR-only semantics)")
	}
}

func TestOverlayRefreshWatchBools(t *testing.T) {
	base := Defaults()
	layer := Config{Refresh: RefreshConfig{WorktreesWatch: true, ReflogWatch: true}}
	overlayRefresh(&base.Refresh, layer.Refresh)
	if !base.Refresh.WorktreesWatch || !base.Refresh.ReflogWatch {
		t.Fatal("watch bools should overlay when true")
	}
	if base.Refresh.BranchesWatch || base.Refresh.RemotesWatch {
		t.Fatal("unset watch bools must stay false")
	}
}

func TestLoadPostCreateHookMultiline(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, ".gg.toml")
	body := "[worktree]\npost_create_hook = '''\ncp \"$GG_MAIN_WORKTREE/.env\" .\nmake setup\n'''\n"
	if err := os.WriteFile(repo, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Join(dir, "none-global.toml"), repo)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := "cp \"$GG_MAIN_WORKTREE/.env\" .\nmake setup\n"
	if cfg.Worktree.PostCreateHook != want {
		t.Fatalf("PostCreateHook = %q, want %q", cfg.Worktree.PostCreateHook, want)
	}
}

func TestOverlayPostCreateHookRepoWins(t *testing.T) {
	dst := WorktreeConfig{PostCreateHook: "global-hook"}
	overlayWorktree(&dst, WorktreeConfig{PostCreateHook: "repo-hook"})
	if dst.PostCreateHook != "repo-hook" {
		t.Fatalf("overlay = %q, want repo-hook", dst.PostCreateHook)
	}
	overlayWorktree(&dst, WorktreeConfig{}) // empty = unset, must not clear
	if dst.PostCreateHook != "repo-hook" {
		t.Fatalf("empty src cleared hook: %q", dst.PostCreateHook)
	}
}
