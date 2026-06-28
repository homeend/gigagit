package gitwatch

import "path/filepath"

// Group is one watched directory plus the predicate deciding which sources a
// change to a given basename within it affects. Events on a directory are
// non-recursive unless Recursive is set (handled by the Watcher in D2).
type Group struct {
	Dir       string
	Recursive bool
	Match     func(base string) []Source
}

// Plan returns the directories to watch for the enabled sources, each with the
// predicate mapping a changed basename to the affected sources. It is pure: all
// .git-layout knowledge lives here. commonDir is the git common dir ($C);
// worktreeDir is the per-worktree git dir ($W) — equal to $C in the main
// worktree. Sources not yet implemented (Branches/Remotes in D1) are ignored.
func Plan(commonDir, worktreeDir string, enabled []Source) []Group {
	var groups []Group
	for _, s := range enabled {
		switch s {
		case Reflog:
			groups = append(groups, Group{
				Dir: filepath.Join(worktreeDir, "logs"),
				Match: func(base string) []Source {
					if base == "HEAD" {
						return []Source{Reflog}
					}
					return nil
				},
			})
		case Worktrees:
			groups = append(groups, Group{
				Dir:   filepath.Join(commonDir, "worktrees"),
				Match: func(base string) []Source { return []Source{Worktrees} },
			})
		}
	}
	return groups
}
