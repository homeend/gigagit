package model

// ConflictClass groups an unmerged file by the resolution actions it supports.
type ConflictClass int

const (
	// ConflictBothSides: both sides hold content (UU, AA) — keep ours/theirs.
	ConflictBothSides ConflictClass = iota
	// ConflictModifyDelete: one side deleted/added the path (DU, UD, AU, UA, DD)
	// — keep the present side / delete / keep base.
	ConflictModifyDelete
)

// conflictCode is the porcelain-v2 two-letter unmerged code (e.g. "UU", "DU").
// The seven codes are fixed mnemonics, not independent per-side flags — 'U' is
// overloaded (ours present in "UD", absent in "UA") — so stage presence is
// derived from the whole code, not byte-by-byte.
func (f FileStatus) conflictCode() string { return string([]byte{f.Staged, f.Unstaged}) }

// ConflictHasOurs reports whether stage 2 (our side) holds content.
func (f FileStatus) ConflictHasOurs() bool {
	switch f.conflictCode() {
	case "AU", "UD", "AA", "UU":
		return true
	}
	return false
}

// ConflictHasTheirs reports whether stage 3 (their side) holds content.
func (f FileStatus) ConflictHasTheirs() bool {
	switch f.conflictCode() {
	case "UA", "DU", "AA", "UU":
		return true
	}
	return false
}

// ConflictHasBase reports whether stage 1 (the common ancestor) exists.
func (f FileStatus) ConflictHasBase() bool {
	switch f.conflictCode() {
	case "DD", "UD", "DU", "UU":
		return true
	}
	return false
}

// ConflictClass classifies the unmerged file. both-sides needs content on both.
func (f FileStatus) ConflictClass() ConflictClass {
	if f.ConflictHasOurs() && f.ConflictHasTheirs() {
		return ConflictBothSides
	}
	return ConflictModifyDelete
}

// ConflictLabel is a plain-language description for the resolution UI.
func (f FileStatus) ConflictLabel() string {
	switch {
	case f.ConflictClass() == ConflictBothSides:
		return "modified on both sides"
	case f.ConflictHasTheirs() && !f.ConflictHasOurs():
		return "deleted by us, modified by them"
	case f.ConflictHasOurs() && !f.ConflictHasTheirs():
		return "modified by us, deleted by them"
	default:
		return "deleted on both sides"
	}
}

// Conflicts returns the unmerged files (git's path order).
func (s WorkingTreeStatus) Conflicts() []FileStatus {
	var out []FileStatus
	for _, f := range s.Files {
		if f.Kind == KindUnmerged {
			out = append(out, f)
		}
	}
	return out
}
