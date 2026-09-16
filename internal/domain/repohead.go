package domain

import "github.com/homeend/gigagit/internal/git"

// RepoHead reports what HEAD points to in the checkout at dir (a short
// branch name, a 7-char sha for a detached HEAD, "" when unreadable), from
// file stats alone — no git process. For repo pickers that list every
// registered checkout; the served repo's branch is CurrentBranch.
func RepoHead(dir string) string { return git.HeadAt(dir) }
