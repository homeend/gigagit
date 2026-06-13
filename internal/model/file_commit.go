package model

// FileCommit is one commit in a single file's history: the commit metadata
// plus the file's status and name *at that commit* (so a diff can address the
// right blob even across renames).
type FileCommit struct {
	Commit         // Hash, Parents, Author, Subject, UnixTime
	Status  string // "A","M","D","R","C","T" — the file's change at this commit
	Path    string // the file's name at this commit (post-rename name)
	OldPath string // parent-side name; set only for renames/copies
}
