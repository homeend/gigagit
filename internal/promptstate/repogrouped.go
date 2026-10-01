package promptstate

// The TUI repo switcher's grouping (ctrl+g in R: each project's checkouts
// under its most recent one). Machine-global UX memory, like StackedDiff.

// RepoGrouped reports whether the TUI repo switcher opens grouped.
func (fs *FileStore) RepoGrouped() bool { return fs.read().RepoGrouped }

// SetRepoGrouped persists the TUI repo switcher's grouping.
func (fs *FileStore) SetRepoGrouped(on bool) error {
	r := fs.read() // read-merge: pick up any sibling writes first
	r.RepoGrouped = on
	return fs.write(r)
}
