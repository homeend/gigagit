package promptstate

// The TUI diff view's stacked mode (the S key: every file of the list in one
// scroll). Machine-local UX memory, like every other record here — and
// deliberately its OWN field, not part of WebUI: the web's stacked_diff and
// this one are independent preferences (stacked-diff design R7), because the
// two frontends are read in different postures and on different screens.

// StackedDiff reports whether the TUI diff view opens stacked.
func (fs *FileStore) StackedDiff() bool { return fs.read().StackedDiff }

// SetStackedDiff persists the TUI stacked-diff preference.
func (fs *FileStore) SetStackedDiff(on bool) error {
	r := fs.read() // read-merge: pick up any sibling writes first
	r.StackedDiff = on
	return fs.write(r)
}

// StackedReviewFiles reports whether the review overview split opens its
// files stacked — its own record, so the diff view and the split are read
// in their own postures (review-overview-split design §4.3).
func (fs *FileStore) StackedReviewFiles() bool { return fs.read().StackedReviewFiles }

// SetStackedReviewFiles persists the review split's stacked preference.
func (fs *FileStore) SetStackedReviewFiles(on bool) error {
	r := fs.read() // read-merge: pick up any sibling writes first
	r.StackedReviewFiles = on
	return fs.write(r)
}
