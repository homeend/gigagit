package tui

import (
	"time"

	"github.com/homeend/gigagit/internal/i18n"
)

// popup_help.go holds the compact per-switcher cheat sheets shown when `?` is
// pressed while the bookmark (g) or shelf (G) quick-switcher is open. Each
// mirrors that popup's bottom hint line, rendered through the generic
// contentPopup viewer (scroll / `/`-search / `z` mode / esc-q close). Also
// holds the prefix-form ctrl+d token/date-format cheat sheet.

// bookmarkSwitcherHelpTitle/shelfSwitcherHelpTitle/prefixTokensHelpTitle are
// funcs, not consts: a const would freeze the translation at package-init
// time (the registries-are-funcs rule — see settingsMenuTitle).
func bookmarkSwitcherHelpTitle() string { return i18n.T("Bookmark switcher (g)") }
func shelfSwitcherHelpTitle() string    { return i18n.T("Shelf switcher (G)") }
func prefixTokensHelpTitle() string     { return i18n.T("Prefix tokens & date formats") }

// cheatRow formats one "key  description" line, key-column padded like the ?
// help table. key is a literal key-cap (never translated); desc is the
// (already-translated) description.
func cheatRow(key, desc string) contentLine {
	return contentLine{text: padRight(key, 12) + desc}
}

// bookmarkSwitcherHelp lists the bookmark switcher's keys. In compare mode
// (opened via the `.`-menu "Compare against bookmark"), the action keys
// p/m/c/x are inert, so the sheet shows only what works.
func bookmarkSwitcherHelp(compare bool) []contentLine {
	if compare {
		return []contentLine{
			cheatRow("↑/k ↓/j", i18n.T("move the selection")),
			cheatRow("enter", i18n.T("compare the first pick against the highlighted bookmark (file vs file, or commit vs commit)")),
			cheatRow("/", i18n.T("filter the list (enter keeps, esc cancels)")),
			cheatRow("ctrl+w", i18n.T("cycle text display: cutoff / wrap / scroll")),
			cheatRow("ctrl+t", i18n.T("toggle fullscreen: wider box, more visible rows")),
			cheatRow("esc", i18n.T("cancel")),
		}
	}
	return []contentLine{
		cheatRow("↑/k ↓/j", i18n.T("move the selection")),
		cheatRow("enter", i18n.T("file bookmark: diff vs the working-tree file; commit bookmark: compare it against the selected commit")),
		cheatRow("e", i18n.T("open the bookmarked file in your external editor, read-only (file bookmarks only)")),
		cheatRow("p", i18n.T("paste the bookmarked file to a path you type (file bookmarks only)")),
		cheatRow("t", i18n.T("copy to a new dir under <repo>.tmp (file or commit bookmarks)")),
		cheatRow("a", i18n.T("cherry-pick a commit bookmark onto the current branch (confirms; the commit must still exist)")),
		cheatRow("y", i18n.T("copy the bookmarked file's path, absolute path, or name to the clipboard (file bookmarks only)")),
		cheatRow("m", i18n.T("mark one, then a second bookmark to compare the two (two files, or two commit bookmarks as a whole-tree compare)")),
		cheatRow("c", i18n.T("compare the highlighted bookmark against a shelf entry (file vs file, or commit vs shelved commit)")),
		cheatRow("x", i18n.T("remove the bookmark (confirms)")),
		cheatRow("/", i18n.T("filter the list (enter keeps, esc cancels)")),
		cheatRow("ctrl+w", i18n.T("cycle text display: cutoff / wrap / scroll")),
		cheatRow("ctrl+t", i18n.T("toggle fullscreen: wider box, more visible rows")),
		cheatRow("esc", i18n.T("close the switcher")),
	}
}

// shelfSwitcherHelp mirrors bookmarkSwitcherHelp for the shelf switcher.
func shelfSwitcherHelp(compare bool) []contentLine {
	if compare {
		return []contentLine{
			cheatRow("↑/k ↓/j", i18n.T("move the selection")),
			cheatRow("enter", i18n.T("compare the first pick against the highlighted entry (file vs file, or commit vs commit)")),
			cheatRow("/", i18n.T("filter the list (enter keeps, esc cancels)")),
			cheatRow("ctrl+w", i18n.T("cycle text display: cutoff / wrap / scroll")),
			cheatRow("ctrl+t", i18n.T("toggle fullscreen: wider box, more visible rows")),
			cheatRow("esc", i18n.T("cancel")),
		}
	}
	return []contentLine{
		cheatRow("↑/k ↓/j", i18n.T("move the selection")),
		cheatRow("enter", i18n.T("file entry: diff vs the working-tree file; shelved commit: browse its files (diff / copy each to the working tree)")),
		cheatRow("e", i18n.T("open the shelved copy in your external editor, read-only (file entries only)")),
		cheatRow("p", i18n.T("restore the shelved copy (path prefilled with the original; ctrl+r re-fills it; file entries only)")),
		cheatRow("t", i18n.T("copy to a new dir under <repo>.tmp")),
		cheatRow("a", i18n.T("cherry-pick a shelved commit onto the current branch (confirms; falls back to its stored patch after a gc)")),
		cheatRow("y", i18n.T("copy the file's path, absolute path, or name to the clipboard (file entries only)")),
		cheatRow("m", i18n.T("mark one, then a second entry to compare the two (two files, or two shelved commits as a whole-tree compare)")),
		cheatRow("c", i18n.T("compare the highlighted entry against a bookmark (file vs file, or shelved commit vs commit bookmark)")),
		cheatRow("x", i18n.T("remove from the shelf (confirms)")),
		cheatRow("/", i18n.T("filter the list (enter keeps, esc cancels)")),
		cheatRow("ctrl+w", i18n.T("cycle text display: cutoff / wrap / scroll")),
		cheatRow("ctrl+t", i18n.T("toggle fullscreen: wider box, more visible rows")),
		cheatRow("esc", i18n.T("close the switcher")),
	}
}

// prefixTokensHelp is the ctrl+d cheat sheet shown over the add-prefix form
// (Settings → Branch prefixes). now feeds the live examples so the sheet
// shows real output, not stale sample dates. Token syntax (the k column, e.g.
// "<date:FMT>") is protocol, never translated; the Examples section's second
// column is a live-formatted date, not prose, so it stays untranslated too.
func prefixTokensHelp(now time.Time) []contentLine {
	tok := func(k, desc string) contentLine {
		return contentLine{text: padRight(k, 22) + desc}
	}
	return []contentLine{
		{text: i18n.T("Tokens"), heading: true},
		tok("<user:LABEL>", i18n.T("asks you for LABEL whenever the prefix is used")),
		tok("<seq:NAME>", i18n.T("per-repo counter NAME (1, 2, …)")),
		tok("<seq:NAME:N>", i18n.T("the same, zero-padded to N digits")),
		tok("<date>", i18n.T("today as yyyy-MM-dd")),
		tok("<date:FMT>", i18n.T("now, formatted by FMT (see below)")),
		tok("<parent-branch>", i18n.T("the branch the new branch forks from")),
		tok("<repo>", i18n.T("the repository directory name")),
		tok("<random-alpha:N>", i18n.T("N random lowercase letters")),
		tok("<random-num:N>", i18n.T("N random digits")),
		{},
		{text: i18n.T("Date format (FMT)"), heading: true},
		tok("yyyy", i18n.T("year, 4 digits")),
		tok("MM", i18n.T("month 01–12")),
		tok("dd", i18n.T("day 01–31")),
		tok("HH", i18n.T("hour 00–23")),
		tok("mm", i18n.T("minute 00–59")),
		tok("ss", i18n.T("second 00–59")),
		{text: i18n.T("Other characters pass through as separators (avoid yyyy/MM/dd/HH/mm/ss")},
		{text: i18n.T("and digits inside literal text — they are format verbs, not literals).")},
		{},
		{text: i18n.T("Examples"), heading: true},
		tok("<date>", now.Format("2006-01-02")),
		tok("<date:yyyy-MM-dd>", now.Format("2006-01-02")),
		tok("<date:yyyyMMdd-HHmm>", now.Format("20060102-1504")),
	}
}
