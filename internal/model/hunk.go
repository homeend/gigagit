package model

// Hunk is one git `@@` region of one file's patch, numbered 1-based in the
// order git prints it. Old/New are 1-based INCLUSIVE line ranges on their side;
// a zero-count side (a pure add has none on the old side, a pure delete none on
// the new) is [0,0], never a degenerate [n,n-1].
//
// This numbering is the agent-facing hunk address (`gg diff --hunks`,
// `gg note add --hunk N`). It is NOT the TUI/web hunk picker's numbering, which
// counts textdiff change blocks and merges no context.
type Hunk struct {
	N      int
	Old    [2]int
	New    [2]int
	Header string
}

// FileHunks is one file's hunk list. Path is the NEW-side, repo-relative path
// in git slash form; OldPath carries a rename/copy source and is empty
// otherwise. A binary file appears with an empty Hunks slice.
type FileHunks struct {
	Path    string
	OldPath string
	Hunks   []Hunk
}
