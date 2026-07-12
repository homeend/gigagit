package tui

import "time"

// popup_help.go holds the compact per-switcher cheat sheets shown when `?` is
// pressed while the bookmark (g) or shelf (G) quick-switcher is open. Each
// mirrors that popup's bottom hint line, rendered through the generic
// contentPopup viewer (scroll / `/`-search / `z` mode / esc-q close). Also
// holds the prefix-form ctrl+d token/date-format cheat sheet.

const bookmarkSwitcherHelpTitle = "Bookmark switcher (g)"
const shelfSwitcherHelpTitle = "Shelf switcher (G)"

// cheatRow formats one "key  description" line, key-column padded like the ?
// help table.
func cheatRow(key, desc string) contentLine {
	return contentLine{text: padRight(key, 12) + desc}
}

// bookmarkSwitcherHelp lists the bookmark switcher's keys. In compare mode
// (opened via the `.`-menu "Compare against bookmark"), the action keys
// p/m/c/x are inert, so the sheet shows only what works.
func bookmarkSwitcherHelp(compare bool) []contentLine {
	if compare {
		return []contentLine{
			cheatRow("↑/k ↓/j", "move the selection"),
			cheatRow("enter", "compare the focused file against the highlighted bookmark"),
			cheatRow("/", "filter the list (enter keeps, esc cancels)"),
			cheatRow("z", "cycle text display: cutoff / wrap / scroll"),
			cheatRow("ctrl+t", "toggle fullscreen: wider box, more visible rows"),
			cheatRow("esc", "cancel"),
		}
	}
	return []contentLine{
		cheatRow("↑/k ↓/j", "move the selection"),
		cheatRow("enter", "file bookmark: diff vs the working-tree file; commit bookmark: compare it against the selected commit"),
		cheatRow("e", "open the bookmarked file in your external editor, read-only (file bookmarks only)"),
		cheatRow("p", "paste the bookmarked file to a path you type (file bookmarks only)"),
		cheatRow("t", "copy to a new dir under <repo>.tmp (file or commit bookmarks)"),
		cheatRow("a", "cherry-pick a commit bookmark onto the current branch (confirms; the commit must still exist)"),
		cheatRow("y", "copy the bookmarked file's path or name to the clipboard (file bookmarks only)"),
		cheatRow("m", "mark one, then a second bookmark to compare the two (file bookmarks only)"),
		cheatRow("c", "compare the highlighted bookmark against a shelf entry (file bookmarks only)"),
		cheatRow("x", "remove the bookmark (confirms)"),
		cheatRow("/", "filter the list (enter keeps, esc cancels)"),
		cheatRow("z", "cycle text display: cutoff / wrap / scroll"),
		cheatRow("ctrl+t", "toggle fullscreen: wider box, more visible rows"),
		cheatRow("esc", "close the switcher"),
	}
}

// shelfSwitcherHelp mirrors bookmarkSwitcherHelp for the shelf switcher.
func shelfSwitcherHelp(compare bool) []contentLine {
	if compare {
		return []contentLine{
			cheatRow("↑/k ↓/j", "move the selection"),
			cheatRow("enter", "compare the focused file against the highlighted entry"),
			cheatRow("/", "filter the list (enter keeps, esc cancels)"),
			cheatRow("z", "cycle text display: cutoff / wrap / scroll"),
			cheatRow("ctrl+t", "toggle fullscreen: wider box, more visible rows"),
			cheatRow("esc", "cancel"),
		}
	}
	return []contentLine{
		cheatRow("↑/k ↓/j", "move the selection"),
		cheatRow("enter", "file entry: diff vs the working-tree file; shelved commit: browse its files (diff / copy each to the working tree)"),
		cheatRow("e", "open the shelved copy in your external editor, read-only (file entries only)"),
		cheatRow("p", "restore the shelved copy (path prefilled with the original; ctrl+r re-fills it; file entries only)"),
		cheatRow("t", "copy to a new dir under <repo>.tmp"),
		cheatRow("a", "cherry-pick a shelved commit onto the current branch (confirms; falls back to its stored patch after a gc)"),
		cheatRow("y", "copy the file's path or name to the clipboard (file entries only)"),
		cheatRow("m", "mark one, then a second entry to compare the two (file entries only)"),
		cheatRow("c", "compare the highlighted entry against a bookmark (file entries only)"),
		cheatRow("x", "remove from the shelf (confirms)"),
		cheatRow("/", "filter the list (enter keeps, esc cancels)"),
		cheatRow("z", "cycle text display: cutoff / wrap / scroll"),
		cheatRow("ctrl+t", "toggle fullscreen: wider box, more visible rows"),
		cheatRow("esc", "close the switcher"),
	}
}

const prefixTokensHelpTitle = "Prefix tokens & date formats"

// prefixTokensHelp is the ctrl+d cheat sheet shown over the add-prefix form
// (Settings → Branch prefixes). now feeds the live examples so the sheet
// shows real output, not stale sample dates.
func prefixTokensHelp(now time.Time) []contentLine {
	tok := func(k, desc string) contentLine {
		return contentLine{text: padRight(k, 22) + desc}
	}
	return []contentLine{
		{text: "Tokens", heading: true},
		tok("<user:LABEL>", "asks you for LABEL whenever the prefix is used"),
		tok("<seq:NAME>", "per-repo counter NAME (1, 2, …)"),
		tok("<seq:NAME:N>", "the same, zero-padded to N digits"),
		tok("<date>", "today as yyyy-MM-dd"),
		tok("<date:FMT>", "now, formatted by FMT (see below)"),
		tok("<parent-branch>", "the branch the new branch forks from"),
		tok("<repo>", "the repository directory name"),
		tok("<random-alpha:N>", "N random lowercase letters"),
		tok("<random-num:N>", "N random digits"),
		{},
		{text: "Date format (FMT)", heading: true},
		tok("yyyy", "year, 4 digits"),
		tok("MM", "month 01–12"),
		tok("dd", "day 01–31"),
		tok("HH", "hour 00–23"),
		tok("mm", "minute 00–59"),
		tok("ss", "second 00–59"),
		{text: "Other characters pass through as separators (avoid yyyy/MM/dd/HH/mm/ss"},
		{text: "and digits inside literal text — they are format verbs, not literals)."},
		{},
		{text: "Examples", heading: true},
		tok("<date>", now.Format("2006-01-02")),
		tok("<date:yyyy-MM-dd>", now.Format("2006-01-02")),
		tok("<date:yyyyMMdd-HHmm>", now.Format("20060102-1504")),
	}
}
