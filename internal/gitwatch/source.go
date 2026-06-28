// Package gitwatch watches a fixed, small set of a repository's .git internal
// files and reports which data sources a change affects. It is the event-driven
// trigger behind gigagit's auto-refresh: instead of polling on a timer, the TUI
// refreshes a panel the moment the .git files backing it change.
//
// The package is pure of gigagit's git/TUI/domain layers — it depends only on
// fsnotify and the standard library — so its .git-layout knowledge (which files
// back which source) is unit-testable in isolation.
package gitwatch

// Source is a watch-eligible data source. gitwatch owns this enum so the package
// stays decoupled from internal/tui's sourceKey; the TUI maps Source→sourceKey.
type Source int

const (
	Branches Source = iota
	Remotes
	Reflog
	Worktrees
)

// String renders a Source for logs/tests.
func (s Source) String() string {
	switch s {
	case Branches:
		return "branches"
	case Remotes:
		return "remotes"
	case Reflog:
		return "reflog"
	case Worktrees:
		return "worktrees"
	}
	return "unknown"
}
