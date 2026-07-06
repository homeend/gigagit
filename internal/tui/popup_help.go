package tui

// popup_help.go holds the compact per-switcher cheat sheets shown when `?` is
// pressed while the bookmark (g) or shelf (G) quick-switcher is open. Each
// mirrors that popup's bottom hint line, rendered through the generic
// contentPopup viewer (scroll / `/`-search / `z` mode / esc-q close).

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
			cheatRow("T", "toggle fullscreen: wider box, more visible rows"),
			cheatRow("esc", "cancel"),
		}
	}
	return []contentLine{
		cheatRow("↑/k ↓/j", "move the selection"),
		cheatRow("enter", "file bookmark: diff vs the working-tree file; commit bookmark: compare it against the selected commit"),
		cheatRow("e", "open the bookmarked file in your external editor, read-only (file bookmarks only)"),
		cheatRow("p", "paste the bookmarked file to a path you type (file bookmarks only)"),
		cheatRow("t", "copy to a new dir under <repo>.tmp (file or commit bookmarks)"),
		cheatRow("m", "mark one, then a second bookmark to compare the two (file bookmarks only)"),
		cheatRow("c", "compare the highlighted bookmark against a shelf entry (file bookmarks only)"),
		cheatRow("x", "remove the bookmark (confirms)"),
		cheatRow("/", "filter the list (enter keeps, esc cancels)"),
		cheatRow("z", "cycle text display: cutoff / wrap / scroll"),
		cheatRow("T", "toggle fullscreen: wider box, more visible rows"),
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
			cheatRow("T", "toggle fullscreen: wider box, more visible rows"),
			cheatRow("esc", "cancel"),
		}
	}
	return []contentLine{
		cheatRow("↑/k ↓/j", "move the selection"),
		cheatRow("enter", "diff the shelved copy vs the working-tree file"),
		cheatRow("e", "open the shelved copy in your external editor, read-only"),
		cheatRow("p", "restore the shelved copy to a path you type"),
		cheatRow("t", "copy to a new dir under <repo>.tmp"),
		cheatRow("m", "mark one, then a second entry to compare the two"),
		cheatRow("c", "compare the highlighted entry against a bookmark"),
		cheatRow("x", "remove from the shelf (confirms)"),
		cheatRow("/", "filter the list (enter keeps, esc cancels)"),
		cheatRow("z", "cycle text display: cutoff / wrap / scroll"),
		cheatRow("T", "toggle fullscreen: wider box, more visible rows"),
		cheatRow("esc", "close the switcher"),
	}
}
