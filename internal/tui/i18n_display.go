package tui

import (
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// opDisplayName translates a sequencer op name for display. The op VALUES
// ("merge", "rebase", "cherry-pick", "revert") are protocol — state,
// config, and comparisons keep the English word; only rendering comes here.
func opDisplayName(op string) string {
	switch op {
	case "merge":
		return i18n.T("merge")
	case "rebase":
		return i18n.T("rebase")
	case "cherry-pick":
		return i18n.T("cherry-pick")
	case "revert":
		return i18n.T("revert")
	}
	return op
}

// describeConflict is the translated sibling of domain.ConflictState.
// Describe: the domain phrase stays English (frontend-agnostic prose); the
// TUI builds its own sentence from the state's fields so word order is the
// translation's to choose.
func describeConflict(c domain.ConflictState) string {
	switch {
	case c.Op == "merge" && c.Source != "" && c.Target != "":
		return i18n.T("merging %s into %s", c.Source, c.Target)
	case c.Op == "rebase" && c.Source != "" && c.Target != "":
		return i18n.T("rebasing %s onto %s", c.Source, c.Target)
	case c.Op == "cherry-pick" && c.Source != "":
		return i18n.T("cherry-picking %s", c.Source)
	case c.Op == "revert" && c.Source != "":
		return i18n.T("reverting %s", c.Source)
	}
	return ""
}

// conflictNotice renders the status-line conflict segment as one complete
// sentence per shape (count × optional source) — no fragment concatenation,
// so every translation controls its own word order and plural form.
func conflictNotice(n int, src string) string {
	switch {
	case n == 1 && src == "":
		return i18n.T("⚠ 1 conflict — press [x] to resolve")
	case n == 1:
		return i18n.T("⚠ 1 conflict %s — press [x] to resolve", src)
	case src == "":
		return i18n.T("⚠ %d conflicts — press [x] to resolve", n)
	default:
		return i18n.T("⚠ %d conflicts %s — press [x] to resolve", n, src)
	}
}

// pausedNotice renders the paused-sequencer segment. op is the DISPLAY name
// (already through opDisplayName); src the describeConflict phrase or "".
func pausedNotice(op, src string) string {
	if src == "" {
		return i18n.T("⏸ %s paused — press [x] to continue or abort", op)
	}
	return i18n.T("⏸ %s paused (%s) — press [x] to continue or abort", op, src)
}
