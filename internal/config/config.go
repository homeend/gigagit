// Package config loads gigagit's global and per-repo TOML configuration and
// manages machine-local per-repo state (the <seq> counters). The committed
// config is read-only at runtime, with one narrow exception: the global file's
// [debug] log_operations key, which the , Settings menu toggle persists via
// SetGlobalDebugLogOperations (a non-destructive line edit).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// WorktreeConfig configures worktree creation. TOML keys are snake_case.
type WorktreeConfig struct {
	PathTemplate          string   `toml:"path_template"`
	DefaultBranchTemplate string   `toml:"default_branch_template"`
	BranchTemplates       []string `toml:"branch_templates"`
	// PostCreateHook is a shell script run after a worktree is created (cwd =
	// the new worktree; env GG_MAIN_WORKTREE/GG_WORKTREE_PATH/GG_BRANCH/GG_REPO).
	// Stored as a multi-line TOML literal ('''…'''). Empty = disabled.
	PostCreateHook string `toml:"post_create_hook"`
}

// UIConfig configures TUI behavior. TOML keys are snake_case.
type UIConfig struct {
	WheelStep     int      `toml:"wheel_step"`     // rows per mouse-wheel tick; <=0 = unset
	HScrollStep   int      `toml:"hscroll_step"`   // diff scroll-mode pan columns per ←/→; <=0 = unset
	FooterActions []string `toml:"footer_actions"` // action ids shown in the footer; empty = all (default)
	MenuActions   []string `toml:"menu_actions"`   // action ids shown in the . menu; empty = all (default)

	SearchHistorySize int `toml:"search_history_size"` // entries kept per search-history ring; <=0 = unset (default 20), clamped to searchhist.MaxSize

	ReflogLimit int `toml:"reflog_limit"` // max HEAD reflog entries shown in the Reflog panel; <=0 = unset (default 200)

	CommitGraphLanes    int `toml:"commit_graph_lanes"`     // default graph window width in lanes; <=0 = unset
	CommitGraphMinLanes int `toml:"commit_graph_min_lanes"` // minimum window width (narrow floor); <=0 = unset
	CommitGraphStep     int `toml:"commit_graph_step"`      // widen/narrow increment in lanes; <=0 = unset
	CommitGraphPanStep  int `toml:"commit_graph_pan_step"`  // pan increment in lanes; <=0 = derived max(1, cols/2)
	CommitGraphMaxLanes int `toml:"commit_graph_max_lanes"` // plane cap in lanes; <=0 = unset; clamped to commitgraph.MaxLanes

	CommitInitialCount   int `toml:"commit_initial_count"`    // commits walked on first paint; <=0 = unset (default 300)
	CommitBatchSize      int `toml:"commit_batch_size"`       // commits per later page (scroll / ctrl+l); <=0 = unset (default 300)
	CommitSearchMaxPages int `toml:"commit_search_max_pages"` // eager /-search page cap before re-prompting; <=0 = unset (default 5)

	// CommitSort selects commit ordering for the Commits panel + graph:
	//   "date-order" — git --date-order: a global topological sort (parent never
	//                  precedes its child ⇒ perfect graph lanes), slower on very
	//                  large repos. THE DEFAULT (used when the key is missing).
	//   "plain"      — git's lazy newest-first order; fastest on huge repos, but
	//                  the graph can draw a disconnected lane stub when date order
	//                  disagrees with topology (e.g. after a squash).
	// Empty = unset (zero-is-unset overlay rule); resolved to the default.
	// Persisted per-repo (.gg.toml) so a huge repo can opt down to "plain".
	CommitSort string `toml:"commit_sort"`

	// ShowGraph selects how the Commits panel renders on startup:
	//   "on"  — the lane graph. THE DEFAULT (used when the key is missing).
	//   "off" — the flat ●-gutter list (same as the . menu's "Show as list").
	// A string (not a bool) on purpose: the zero-is-unset overlay rule would make
	// a bool's `false` indistinguishable from unset, so a repo could never turn
	// the graph back on over a global off. Empty = unset; resolved to "on".
	// Persisted per-repo (.gg.toml) by the Settings "Show graph" toggle.
	ShowGraph string `toml:"show_graph"`

	// Language selects the TUI display language: empty/"en" = English,
	// "ja"/"ko"/"zh"/"ru" built in, or a custom code matching a file in
	// $XDG_CONFIG_HOME/gg/lang/<code>.toml. CLI output is always English.
	Language string `toml:"language"`

	ShowEOLOnlyChanges bool `toml:"show_eol_only_changes"` // surface files whose only unstaged change is line endings (CRLF↔LF); false (default) hides them as noise

	// DisableSlowOpConfirm turns OFF the yes/no confirmation shown before slow
	// working-tree operations (switch, checkout, pull, merge, rebase,
	// fast-forward, reset). Inverted polarity: default false ⇒ confirmation ON;
	// only a true in a higher layer overlays (matching the zero-is-unset rule).
	DisableSlowOpConfirm bool `toml:"disable_slow_op_confirm"`
}

// DebugConfig configures diagnostic logging. TOML keys are snake_case.
type DebugConfig struct {
	// LogOperations mirrors every operation span and git invocation (redacted)
	// to the operation log file, so a hung or slow op leaves a trace. Default
	// false (off). Inverted polarity: only a true in a higher layer overlays.
	LogOperations bool `toml:"log_operations"`
}

// RefreshConfig configures background auto-refresh. All off by default.
// Enabled is the master gate; each interval is seconds (0 = that source never
// auto-refreshes). TOML keys snake_case under [refresh].
type RefreshConfig struct {
	Enabled    bool `toml:"enabled"` // master switch; default false (whole feature off)
	Status     int  `toml:"status"`  // seconds between background status reads; 0 = off
	Branches   int  `toml:"branches"`
	Remotes    int  `toml:"remotes"`
	Worktrees  int  `toml:"worktrees"`
	Tags       int  `toml:"tags"`
	Reflog     int  `toml:"reflog"`
	Feed       int  `toml:"feed"`
	Fetch      int  `toml:"fetch"`       // seconds between background `git fetch`; 0 = off
	RemoteTags int  `toml:"remote_tags"` // seconds between background remote-tag (ls-remote) lookups; 0 = off

	// MinSeconds is the floor on any auto-refresh interval: no source polls more
	// often than this, even when a source reads very cheaply. 0 = unset → default 10.
	MinSeconds int `toml:"min_seconds"`

	// DisableRemoteTagsAuto turns OFF the automatic remote-tag refresh that runs
	// when the tag list changes (app load + after tag add/remove/push). Inverted
	// polarity: default false = auto-refresh ON. Independent of Enabled.
	DisableRemoteTagsAuto bool `toml:"disable_remote_tags_auto"`

	// Per-source file-watch toggles (Phase D). When true AND the repo's fs
	// supports inotify (not WSL2 9p drvfs), the source refreshes on .git file
	// change instead of on its interval; otherwise it falls back to the interval.
	WorktreesWatch bool `toml:"worktrees_watch"`
	BranchesWatch  bool `toml:"branches_watch"`
	ReflogWatch    bool `toml:"reflog_watch"`
	RemotesWatch   bool `toml:"remotes_watch"`
}

// Config is the merged gigagit configuration.
type Config struct {
	Worktree WorktreeConfig `toml:"worktree"`
	UI       UIConfig       `toml:"ui"`
	Debug    DebugConfig    `toml:"debug"`
	Refresh  RefreshConfig  `toml:"refresh"`
	Tools    ToolsConfig    `toml:"tools"`
}

// Defaults returns the built-in configuration used when no files set a field.
func Defaults() Config {
	return Config{
		Worktree: WorktreeConfig{
			PathTemplate:          "../<repo>.worktrees/<branch>",
			DefaultBranchTemplate: "b/from-<parent-branch>-<random-alpha:4>",
		},
		UI: UIConfig{WheelStep: 3, HScrollStep: 8, CommitGraphLanes: 8, CommitGraphMinLanes: 2, CommitGraphStep: 4,
			CommitInitialCount: 300, CommitBatchSize: 300, CommitSearchMaxPages: 5, CommitSort: "date-order", ShowGraph: "on"},
	}
}

// Load builds the effective Config: built-in defaults, overlaid by any field the
// global file sets, then overlaid by any field the repo file sets (repo wins).
// A missing file is skipped (not an error); a present-but-malformed file errors.
func Load(globalPath, repoPath string) (Config, error) {
	cfg := Defaults()

	for _, path := range []string{globalPath, repoPath} {
		layer, ok, err := decodeFile(path)
		if err != nil {
			return Config{}, err
		}
		if ok {
			overlayWorktree(&cfg.Worktree, layer.Worktree)
			overlayUI(&cfg.UI, layer.UI)
			overlayDebug(&cfg.Debug, layer.Debug)
			overlayRefresh(&cfg.Refresh, layer.Refresh)
			overlayTools(&cfg.Tools, layer.Tools)
		}
	}
	return cfg, nil
}

// FileUILanguage reads one config file's [ui] language value ("" when the
// file is missing, unreadable, or the key is unset). The TUI language
// picker uses it to warn that a repo-level key overrides the global choice
// the picker writes.
func FileUILanguage(path string) string {
	if path == "" {
		return ""
	}
	c, ok, err := decodeFile(path)
	if err != nil || !ok {
		return ""
	}
	return c.UI.Language
}

// decodeFile reads and decodes one config file. ok is false (no error) when the
// file does not exist.
func decodeFile(path string) (Config, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return Config{}, false, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	return c, true, nil
}

// overlayWorktree copies each non-empty field of src onto dst (field-level
// overlay: an unset field in src leaves dst untouched). A field left empty in
// src is treated as unset, so a higher layer cannot deliberately reset a lower
// layer's field to empty; this is intentional.
func overlayWorktree(dst *WorktreeConfig, src WorktreeConfig) {
	if src.PathTemplate != "" {
		dst.PathTemplate = src.PathTemplate
	}
	if src.DefaultBranchTemplate != "" {
		dst.DefaultBranchTemplate = src.DefaultBranchTemplate
	}
	if len(src.BranchTemplates) > 0 {
		dst.BranchTemplates = src.BranchTemplates
	}
	if src.PostCreateHook != "" {
		dst.PostCreateHook = src.PostCreateHook
	}
}

// overlayUI copies each set field of src onto dst. WheelStep <= 0 is unset
// (same rule as the string fields: a higher layer cannot reset a lower
// layer's value to the zero value).
func overlayUI(dst *UIConfig, src UIConfig) {
	if src.WheelStep > 0 {
		dst.WheelStep = src.WheelStep
	}
	if src.HScrollStep > 0 {
		dst.HScrollStep = src.HScrollStep
	}
	if len(src.FooterActions) > 0 {
		dst.FooterActions = src.FooterActions
	}
	if len(src.MenuActions) > 0 {
		dst.MenuActions = src.MenuActions
	}
	if src.SearchHistorySize > 0 {
		dst.SearchHistorySize = src.SearchHistorySize
	}
	if src.ReflogLimit > 0 {
		dst.ReflogLimit = src.ReflogLimit
	}
	if src.CommitGraphLanes > 0 {
		dst.CommitGraphLanes = src.CommitGraphLanes
	}
	if src.CommitGraphMinLanes > 0 {
		dst.CommitGraphMinLanes = src.CommitGraphMinLanes
	}
	if src.CommitGraphStep > 0 {
		dst.CommitGraphStep = src.CommitGraphStep
	}
	if src.CommitGraphPanStep > 0 {
		dst.CommitGraphPanStep = src.CommitGraphPanStep
	}
	if src.CommitGraphMaxLanes > 0 {
		dst.CommitGraphMaxLanes = src.CommitGraphMaxLanes
	}
	if src.CommitInitialCount > 0 {
		dst.CommitInitialCount = src.CommitInitialCount
	}
	if src.CommitBatchSize > 0 {
		dst.CommitBatchSize = src.CommitBatchSize
	}
	if src.CommitSearchMaxPages > 0 {
		dst.CommitSearchMaxPages = src.CommitSearchMaxPages
	}
	if src.CommitSort != "" {
		dst.CommitSort = src.CommitSort
	}
	if src.ShowGraph != "" {
		dst.ShowGraph = src.ShowGraph
	}
	if src.Language != "" {
		dst.Language = src.Language
	}
	// Inverted polarity: the default (false) is the active feature (hide), so
	// only a true in a higher layer overlays — matching the zero-is-unset rule.
	if src.ShowEOLOnlyChanges {
		dst.ShowEOLOnlyChanges = true
	}
	if src.DisableSlowOpConfirm {
		dst.DisableSlowOpConfirm = true
	}
}

// overlayDebug copies each set field of src onto dst. Inverted polarity: the
// default (false) is "off", so only a true in a higher layer overlays —
// matching the zero-is-unset rule used elsewhere.
func overlayDebug(dst *DebugConfig, src DebugConfig) {
	if src.LogOperations {
		dst.LogOperations = true
	}
}

// overlayRefresh copies each set field of src onto dst. Intervals use the
// zero-is-unset rule (0 = unset). Enabled uses inverted polarity: default false
// is "off", so only a true in a higher layer overlays (a higher layer cannot
// reset a lower layer's true back to false — matching LogOperations).
func overlayRefresh(dst *RefreshConfig, src RefreshConfig) {
	if src.Enabled {
		dst.Enabled = true
	}
	if src.Status > 0 {
		dst.Status = src.Status
	}
	if src.Branches > 0 {
		dst.Branches = src.Branches
	}
	if src.Remotes > 0 {
		dst.Remotes = src.Remotes
	}
	if src.Worktrees > 0 {
		dst.Worktrees = src.Worktrees
	}
	if src.Tags > 0 {
		dst.Tags = src.Tags
	}
	if src.Reflog > 0 {
		dst.Reflog = src.Reflog
	}
	if src.Feed > 0 {
		dst.Feed = src.Feed
	}
	if src.Fetch > 0 {
		dst.Fetch = src.Fetch
	}
	if src.RemoteTags > 0 {
		dst.RemoteTags = src.RemoteTags
	}
	if src.MinSeconds > 0 {
		dst.MinSeconds = src.MinSeconds
	}
	if src.DisableRemoteTagsAuto {
		dst.DisableRemoteTagsAuto = true
	}
	if src.WorktreesWatch {
		dst.WorktreesWatch = true
	}
	if src.BranchesWatch {
		dst.BranchesWatch = true
	}
	if src.ReflogWatch {
		dst.ReflogWatch = true
	}
	if src.RemotesWatch {
		dst.RemotesWatch = true
	}
}

// configHome returns the base config directory: $XDG_CONFIG_HOME, else
// ~/.config (empty home ⇒ ".config" relative). Shared by DefaultGlobalPath and
// PrivateRepoPath so the two paths always live under the same root.
func configHome() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = ""
		}
		base = filepath.Join(home, ".config")
	}
	return base
}

// DefaultGlobalPath returns the global config path, honoring $XDG_CONFIG_HOME
// and falling back to ~/.config/gg/config.toml.
func DefaultGlobalPath() string {
	return filepath.Join(configHome(), "gg", "config.toml")
}

// LangDir is the machine-local custom-language bundle directory. A file
// <code>.toml here overlays the embedded bundle of the same code per-key,
// or adds a brand-new language.
func LangDir() string {
	return filepath.Join(configHome(), "gg", "lang")
}

// EncodeRepoKey turns an absolute repo path into a filesystem-safe, readable
// directory name by replacing every path separator and drive colon (/, \, :)
// with '-'. So /mnt/t/others/gigagit -> -mnt-t-others-gigagit and C:\src\repo
// -> C--src-repo. Empty in yields empty out (the caller must guard).
func EncodeRepoKey(repoPath string) string {
	if repoPath == "" {
		return ""
	}
	cleaned := filepath.Clean(repoPath)
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':':
			return '-'
		}
		return r
	}, cleaned)
}

// PrivateRepoPath returns the machine-local per-repo config path for a repo
// whose MAIN worktree is at mainWorktreePath:
// $XDG_CONFIG_HOME/gg/projects/<encoded>/config.toml. Returns "" if
// mainWorktreePath is "" (no anchor ⇒ no private path). Anchored on the main
// worktree so every linked worktree of a repo shares one private config.
func PrivateRepoPath(mainWorktreePath string) string {
	if mainWorktreePath == "" {
		return ""
	}
	return filepath.Join(configHome(), "gg", "projects", EncodeRepoKey(mainWorktreePath), "config.toml")
}
