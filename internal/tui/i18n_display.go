package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// padCell right-pads s to display width w using lipgloss.Width (rune-width
// aware), not byte count. fmt's %-Ns pads by BYTE length, which misaligns a
// translated CJK cell in the Rates editor's fixed-width columns (a CJK rune
// is ~3 bytes but only 2 display cells) — this is the alignment-safe
// replacement for the translated columns there.
func padCell(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

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

// sourceDisplayName translates a data-source name for status-line display.
// sourceNames stays English (identity, error-log prose); this is the
// render-time lookup. The switch is literal-key so the scan test sees every
// key.
func sourceDisplayName(s sourceKey) string {
	switch s {
	case srcStatus:
		return i18n.T("status")
	case srcBranches:
		return i18n.T("branches")
	case srcRemotes:
		return i18n.T("remotes")
	case srcTags:
		return i18n.T("tags")
	case srcReflog:
		return i18n.T("reflog")
	case srcWorktrees:
		return i18n.T("worktrees")
	case srcFeed:
		return i18n.T("commits")
	case srcIdentity:
		return i18n.T("identity")
	}
	return sourceNames[s]
}

// optionDisplayName translates a decision-option label for display in the
// modal. Option VALUES are protocol — Options lists, onResolve/decider
// comparisons, and the esc→"abort" mapping keep the English word; ONLY
// renderModal's option loop comes here. A value with no case (e.g. a
// dynamic name) passes through — names must not be translated anyway.
// Task: internal/tui/options_vocab_test.go forces every statically declared
// option value through the bundles; the bundle orphan check forces each
// bundle key back to a T() literal, which is the case arm here.
func optionDisplayName(value string) string {
	switch value {
	case "Apply patch":
		return i18n.T("Apply patch")
	case "Cancel":
		return i18n.T("Cancel")
	case "Cherry-pick":
		return i18n.T("Cherry-pick")
	case "Copy absolute file path":
		return i18n.T("Copy absolute file path")
	case "Copy file name":
		return i18n.T("Copy file name")
	case "Copy file path":
		return i18n.T("Copy file path")
	case "Create branch…":
		return i18n.T("Create branch…")
	case "Create worktree…":
		return i18n.T("Create worktree…")
	case "Delete":
		return i18n.T("Delete")
	case "Detached":
		return i18n.T("Detached")
	case "Discard":
		return i18n.T("Discard")
	case "No":
		return i18n.T("No")
	case "Push branch + tags":
		return i18n.T("Push branch + tags")
	case "Push branch only":
		return i18n.T("Push branch only")
	case "Remove":
		return i18n.T("Remove")
	case "Reorder & squash":
		return i18n.T("Reorder & squash")
	case "Yes":
		return i18n.T("Yes")
	case "abort":
		return i18n.T("abort")
	case "cancel":
		return i18n.T("cancel")
	case "check out as different name…":
		return i18n.T("check out as different name…")
	case "checkout-and-resolve":
		return i18n.T("checkout-and-resolve")
	case "commits":
		return i18n.T("commits")
	case "delete":
		return i18n.T("delete")
	case "force":
		return i18n.T("force")
	case "force-delete":
		return i18n.T("force-delete")
	case "force-with-lease":
		return i18n.T("force-with-lease")
	case "go to worktree":
		return i18n.T("go to worktree")
	case "hard":
		return i18n.T("hard")
	case "keep":
		return i18n.T("keep")
	case "keep-conflicts":
		return i18n.T("keep-conflicts")
	case "merge":
		return i18n.T("merge")
	case "mixed":
		return i18n.T("mixed")
	case "no":
		return i18n.T("no")
	case "overwrite":
		return i18n.T("overwrite")
	case "proceed":
		return i18n.T("proceed")
	case "pull now":
		return i18n.T("pull now")
	case "rebase":
		return i18n.T("rebase")
	case "reset":
		return i18n.T("reset")
	case "run":
		return i18n.T("run")
	case "skip":
		return i18n.T("skip")
	case "soft":
		return i18n.T("soft")
	case "unlock-and-remove":
		return i18n.T("unlock-and-remove")
	case "working-tree":
		return i18n.T("working-tree")
	case "worktree-and-branch":
		return i18n.T("worktree-and-branch")
	case "worktree-only":
		return i18n.T("worktree-only")
	case "yes":
		return i18n.T("yes")
	}
	return value
}
