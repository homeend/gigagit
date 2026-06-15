package model

// RangeCommit is a commit in a rev-range with its full message, used by the
// interactive-rebase editor (reword pre-fill + squash compose need the body).
type RangeCommit struct {
	Hash    string
	Subject string
	Message string // full message (subject + body), trailing newline preserved
}
