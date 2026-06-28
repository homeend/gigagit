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
// worktree.
func Plan(commonDir, worktreeDir string, enabled []Source) []Group {
	on := map[Source]bool{}
	for _, s := range enabled {
		on[s] = true
	}
	var groups []Group

	if on[Reflog] {
		groups = append(groups, Group{
			Dir: filepath.Join(worktreeDir, "logs"),
			Match: func(base string) []Source {
				if base == "HEAD" {
					return []Source{Reflog}
				}
				return nil
			},
		})
	}
	if on[Worktrees] {
		groups = append(groups, Group{
			Dir:   filepath.Join(commonDir, "worktrees"),
			Match: func(base string) []Source { return []Source{Worktrees} },
		})
	}
	if on[Branches] {
		groups = append(groups, Group{
			Dir: filepath.Join(commonDir, "refs", "heads"), Recursive: true,
			Match: func(base string) []Source { return []Source{Branches} },
		})
	}
	if on[Remotes] {
		groups = append(groups, Group{
			Dir: filepath.Join(commonDir, "refs", "remotes"), Recursive: true,
			Match: func(base string) []Source { return []Source{Remotes} },
		})
	}
	// Shared commonDir group: packed-refs (branches+remotes), FETCH_HEAD/config (remotes).
	if on[Branches] || on[Remotes] {
		groups = append(groups, Group{
			Dir: commonDir,
			Match: func(base string) []Source {
				var out []Source
				switch base {
				case "packed-refs":
					if on[Branches] {
						out = append(out, Branches)
					}
					if on[Remotes] {
						out = append(out, Remotes)
					}
				case "FETCH_HEAD", "config":
					if on[Remotes] {
						out = append(out, Remotes)
					}
				}
				return out
			},
		})
	}
	// Worktree HEAD affects the branches view (current-branch line).
	if on[Branches] {
		groups = append(groups, Group{
			Dir: worktreeDir,
			Match: func(base string) []Source {
				if base == "HEAD" {
					return []Source{Branches}
				}
				return nil
			},
		})
	}
	return groups
}
