package tui

import (
	"path/filepath"
	"strings"
)

// filePathKind selects which surface a filePathPopup opens on submit.
type filePathKind int

const (
	filePathHistory filePathKind = iota
	filePathBlame
)

// repoRelPath turns user-typed input into the repo-relative, forward-slashed
// path the git verbs expect. An absolute path inside root is reduced to its
// repo-relative form; anything else is cleaned and slashed as-is. Blank stays
// blank. A path that escapes the repo (../…) falls back to the cleaned input —
// git then reports no history rather than the popup hard-failing.
func repoRelPath(root, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if root != "" && filepath.IsAbs(p) {
		if rel, err := filepath.Rel(root, p); err == nil && !escapesRepo(rel) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filepath.Clean(p))
}

func escapesRepo(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
