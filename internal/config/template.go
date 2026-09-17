package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/theme"
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
	{"worktree", "path_template", "../<repo>.worktrees/<branch>", "where gg worktree creates new worktrees (tokens: <repo> <branch> <parent-branch> <date> <date:…> <seq:…>)"},
	{"worktree", "default_branch_template", "<parent-branch>-<date:yyyy-MM-dd_HH-mm>", "auto branch name for a new worktree"},
	{"worktree", "branch_templates", nil, "extra branch-name templates offered in the worktree popup (default: none)"},
	{"worktree", "post_create_hook", nil, "shell script run after creating a worktree (cwd=new worktree; env GG_MAIN_WORKTREE/GG_WORKTREE_PATH/GG_BRANCH/GG_REPO); multi-line '''…''' literal; default: none"},

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
	{"ui", "commit_search_max_pages", 50, "pages eager /-search scans before asking to search deeper"},
	{"ui", "commit_sort", "date-order", "commit ordering for the Commits panel + graph: date-order (default; git --date-order, perfect graph lanes) or plain (fastest on huge repos, but the graph can mis-draw forks)"},
	{"ui", "diff_syntax", "auto", "syntax colouring in diff views: auto (by file name) or off"},
	{"ui", "diff_cursor", "row", "current-line marker in the diff view (on the cursor's side only) and the View file preview: row (background), number (gutter only) or off (the diff view's . menu Cursor marker row switches it for the session; the View file preview follows the same setting, number falling back to the band — it has no gutter to carry a number)"},
	{"ui", "show_graph", "on", "Commits panel render mode on startup: on (default; lane graph) or off (flat list, same as the . menu's Show as list); toggle live from the , Settings menu"},
	{"ui", "language", nil, "TUI display language: en (default), ja, ko, zh, ru, or a custom code from $XDG_CONFIG_HOME/gg/lang/<code>.toml; pick from the , Settings menu (CLI output stays English)"},
	{"ui", "theme", "terminal", "TUI colours: terminal (default; inherit the terminal's own scheme), dark (Windows Terminal Campbell look, pinned everywhere), light (neutral light grey, charcoal text); cycle from the , Settings menu. To retune a theme's individual colours, run `gg config populate` — it writes a commented [themes.<name>] override table per theme"},
	{"ui", "agent_steering", "on", "accept live-steering commands from `gg session` (an AI agent putting your window on the line it just annotated): on (default) or off; with off the TUI and gg web write no session presence and gg session reports no session"},

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
	{"refresh", "remote_tags", 0, "seconds between background remote-tag (ls-remote) lookups; 0 = off"},
	{"refresh", "min_seconds", 10, "floor on any auto-refresh interval; no source polls more often than this"},
	{"refresh", "disable_remote_tags_auto", false, "disable auto remote-tag refresh on tag-list changes (default: on)"},

	{"refresh", "worktrees_watch", false, "refresh worktrees on .git file change (off → use interval); ignored on WSL2 9p mounts"},
	{"refresh", "branches_watch", false, "refresh branches on ref change (off → use interval); ignored on WSL2 9p mounts"},
	{"refresh", "reflog_watch", false, "refresh reflog on logs/HEAD change (off → use interval); ignored on WSL2 9p mounts"},
	{"refresh", "remotes_watch", false, "refresh remotes on ref/FETCH_HEAD change (off → use interval); ignored on WSL2 9p mounts"},

	{"versions", "disabled", false, "disable branch-version snapshots before merges/rebases (default: on)"},
	{"versions", "max_age_days", 90, "prune branch versions older than this many days; -1 = keep forever"},

	{"notes", "max_age_days", 30, "prune review notes older than this many days (the sweep at every gg start also drops notes whose anchor is gone); -1 = keep forever"},
	{"notes", "max_entries", 2000, "cap on stored review notes, enforced on every write (oldest thread dropped first); -1 = uncapped"},

	{"branches", "filter", nil, "branch filters as [[branches.filter]] blocks (alt+1…5 in the Branches/Remotes lists): slot (1..5), name, mode (hide | show = show only matching), older_than / younger_than (tip age: 90d 12w 6m 1y), prefix, suffix, contains, regex (Go RE2); set clauses AND together; a repo block REPLACES the global block for the same slot; invalid blocks are inert with a reason; edit from Settings → Branch filters… (TUI) or the web settings view"},

	{"tools", "command", nil, "external-tool commands as [[tools.command]] blocks: category (conflict|commit_message|review|conflict_complete), name, mode (terminal|capture), per_file, when_op, command (multi-line '''…''' literal; tokens: <op> <source> <target> <conflicted-files> <repo> <file> <local> <base> <remote> <merged> <context-file> <user:LABEL>); global + repo lists CONCATENATE, repo wins a (category,name) collision; generate defaults via Settings → External tools; values substitute literally — prefer \"$GG_*\" env vars or <context-file> when values may contain shell metacharacters. A commit_message command normally uses mode=\"capture\" (runs headless, its stdout is captured and parsed into a commit subject+body for the commit popup's ctrl+g) and reads the staged diff via two env vars instead of a token: $GG_CONTEXT_FILE (a labeled summary — files changed, recent-commit style) and $GG_STAGED_DIFF (the full `git diff --cached`, truncated past a size cap)"},
}

// themeNote is the trailing comment on each generated [themes.<name>] header.
func themeNote(name string) string {
	if name == theme.NameTerminal {
		return "every role empty = inherit the terminal's own scheme; anything set here paints over it (bg/fg paint the frame)"
	}
	return "colour overrides for the " + name + " theme — uncomment a table and only the roles you want to repaint"
}

// themeBlock renders one commented [themes.<name>] example block: the header,
// then every role of RoleDocs with th's current value and its description.
// Every line is commented, so the block is inert until a user uncomments it.
func themeBlock(th theme.Theme) []string {
	o := th.AsOverride()
	docs := theme.RoleDocs()

	assign := make([]string, len(docs))
	width := 0
	for i, d := range docs {
		switch d.Key {
		case "lanes":
			assign[i] = d.Key + " = " + tomlStringList(o.Lanes)
		case "syntax":
			assign[i] = d.Key + " = " + tomlStringList(o.Syntax)
		default:
			assign[i] = d.Key + " = " + tomlScalar(o.Value(d.Key))
			if n := len(assign[i]); n > width {
				width = n // align the comment column over the scalars only
			}
		}
	}

	lines := []string{"# [themes." + th.Name + "]   # " + themeNote(th.Name) + " [populated]"}
	for i, d := range docs {
		pad := ""
		if n := width - len(assign[i]); n > 0 {
			pad = strings.Repeat(" ", n)
		}
		lines = append(lines, "# "+assign[i]+pad+"   # "+d.Doc+" [populated]")
	}
	return lines
}

// themeBlockDoc is one theme's generated block plus the name populate matches
// against the file to decide whether it is already there.
type themeBlockDoc struct {
	name  string
	lines []string
}

// themeDocs renders the example block for every built-in theme, light first
// (the one users most often retune), then dark, then terminal.
func themeDocs() []themeBlockDoc {
	out := make([]themeBlockDoc, 0, 3)
	for _, th := range []theme.Theme{theme.Light, theme.Dark, theme.Terminal} {
		out = append(out, themeBlockDoc{name: th.Name, lines: themeBlock(th)})
	}
	return out
}

// tomlStringList renders a []string as a TOML inline array.
func tomlStringList(v []string) string {
	parts := make([]string, len(v))
	for i, s := range v {
		parts[i] = `"` + s + `"`
	}
	return "[" + strings.Join(parts, ", ") + "]"
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
	for _, section := range []string{"worktree", "ui", "debug", "refresh", "versions", "notes", "branches", "tools"} {
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
