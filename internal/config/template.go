package config

import (
	"fmt"
	"strconv"
	"strings"
)

// settingDoc documents one configuration setting for the generated template
// (`gg config init`). value is the default rendered into the file: an int or
// string for a concrete default, or nil when the setting has no honest scalar
// default (derived, or "empty = all"), in which case it renders comment-only.
//
// settingDocs is the SINGLE SOURCE OF TRUTH for the generated config file. When
// you add a [ui]/[worktree] setting, add its entry here — TestSettingDocsCoverAllFields
// (config/template_test.go) FAILS until you do.
type settingDoc struct {
	section string // "worktree" or "ui"
	key     string // toml key
	value   any    // int | string | nil (nil ⇒ comment-only, no "= value")
	comment string // one-line description; states the default when value is nil
}

var settingDocs = []settingDoc{
	{"worktree", "path_template", "../<repo>.worktrees/<branch>", "where gg worktree creates new worktrees (tokens: <repo> <branch> <parent-branch> <date:…> <seq:…>)"},
	{"worktree", "default_branch_template", "b/from-<parent-branch>-<random-alpha:4>", "auto branch name for a new worktree"},
	{"worktree", "branch_templates", nil, "extra branch-name templates offered in the worktree popup (default: none)"},

	{"ui", "wheel_step", 3, "mouse-wheel scroll step, in rows"},
	{"ui", "hscroll_step", 8, "diff scroll-mode horizontal pan step, in columns"},
	{"ui", "footer_actions", nil, "action ids shown in the footer bar (default: empty = show all)"},
	{"ui", "menu_actions", nil, "action ids shown in the . menu (default: empty = show all)"},
	{"ui", "search_history_size", 20, "phrases kept per search-history ring (max 1000)"},
	{"ui", "reflog_limit", 200, "max HEAD reflog entries the Reflog tab loads"},
	{"ui", "commit_graph_lanes", 8, "default commit-graph window width, in lanes"},
	{"ui", "commit_graph_min_lanes", 2, "minimum commit-graph window width (narrow floor)"},
	{"ui", "commit_graph_step", 4, "commit-graph widen/narrow increment, in lanes"},
	{"ui", "commit_graph_pan_step", nil, "commit-graph pan increment, in lanes (default: derived, max(1, cols/2))"},
	{"ui", "commit_graph_max_lanes", 320, "commit-graph plane cap, in lanes (config can only lower the 320 ceiling)"},

	{"ui", "commit_initial_count", 300, "commits loaded on first paint (raise to find more without scrolling)"},
	{"ui", "commit_batch_size", 300, "commits loaded per later page (scroll to the end, or ctrl+l)"},
	{"ui", "commit_search_max_pages", 5, "pages eager /-search scans before asking to search deeper"},

	{"ui", "show_eol_only_changes", false, "show files whose only unstaged change is line endings (CRLF↔LF); default hides them as noise"},
	{"ui", "disable_slow_op_confirm", false, "skip the yes/no confirmation shown before slow working-tree ops (switch, checkout, pull, merge, rebase, fast-forward, reset)"},

	{"debug", "log_operations", false, "mirror every op + git invocation (redacted) to the operation log; toggle live from the , Settings menu"},

	{"refresh", "enabled", false, "master switch for background auto-refresh (all sources); default false = feature off"},
	{"refresh", "status", 0, "seconds between background status reads; 0 = off"},
	{"refresh", "branches", 0, "seconds between background branch-list refresh; 0 = off"},
	{"refresh", "remotes", 0, "seconds between background remote-branch refresh; 0 = off"},
	{"refresh", "worktrees", 0, "seconds between background worktree-list refresh; 0 = off"},
	{"refresh", "tags", 0, "seconds between background tag-list refresh; 0 = off"},
	{"refresh", "reflog", 0, "seconds between background reflog refresh; 0 = off"},
	{"refresh", "feed", 0, "seconds between background commit-feed refresh; 0 = off"},
	{"refresh", "fetch", 0, "seconds between background `git fetch`; 0 = off"},
	{"refresh", "disable_adaptive", false, "turn OFF adaptive intervals (Phase C); default false = adaptive on, each source's interval auto-tunes from its measured read time"},
	{"refresh", "max_read_seconds", 10, "a source whose average read exceeds this many seconds drops out of auto-refresh (manual r only)"},
	{"refresh", "backoff_factor", 10, "effective interval = max(configured, this × average read seconds)"},
}

// tomlScalar renders a registry value as it appears in TOML.
func tomlScalar(v any) string {
	switch t := v.(type) {
	case int:
		return strconv.Itoa(t)
	case bool:
		return strconv.FormatBool(t)
	case string:
		return `"` + t + `"`
	}
	return ""
}

// Template renders the commented config file: a header, then [worktree] and
// [ui] sections in settingDocs order. Every line is commented, so writing the
// file changes nothing until a line is uncommented.
func Template() string {
	var b strings.Builder
	b.WriteString("# gg configuration — every setting with its default.\n")
	b.WriteString("# Uncomment a line to override the default. Values shown are gg's built-in\n")
	b.WriteString("# defaults; leaving a line commented keeps tracking the default across versions.\n")
	for _, section := range []string{"worktree", "ui", "debug", "refresh"} {
		b.WriteString("\n[" + section + "]\n")
		for _, d := range settingDocs {
			if d.section != section {
				continue
			}
			if d.value == nil {
				fmt.Fprintf(&b, "# %s   # %s\n", d.key, d.comment)
			} else {
				fmt.Fprintf(&b, "# %s = %s   # %s\n", d.key, tomlScalar(d.value), d.comment)
			}
		}
	}
	return b.String()
}
