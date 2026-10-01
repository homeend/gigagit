package git

import (
	"os"
	"path/filepath"
	"strings"
)

// HeadAt reports what HEAD points to in the working tree at dir, read from
// the files alone — no git invocation, like PausedOpIn and LockFiles. A
// branch comes back as its short name ("main"), a detached HEAD as a
// 7-character sha prefix, and anything unreadable (not a repo, a dangling
// `gitdir:` file, a missing dir) as "". It exists for surfaces that list
// MANY repositories at once (the repo switcher) and cannot afford a git
// process per row; the served repo's own branch comes from CurrentBranch.
//
// Layout handled: a `.git` directory (a primary worktree) and a `.git` FILE
// holding `gitdir: <path>` (a linked worktree; the path may be relative to
// dir). HEAD itself is either `ref: refs/heads/<name>` or a raw sha.
func HeadAt(dir string) string {
	gitDir := GitDirAt(dir)
	if gitDir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(raw))
	if ref, ok := strings.CutPrefix(head, "ref:"); ok {
		ref = strings.TrimSpace(ref)
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	if len(head) > 7 {
		head = head[:7]
	}
	return head
}

// GitDirAt is the git directory of the working tree at dir, read from the
// files alone: <dir>/.git when it is a directory (a primary worktree), else
// the `gitdir:` target of the .git FILE (a linked worktree; relative targets
// resolve against dir). "" when dir is not a working tree.
func GitDirAt(dir string) string {
	gitDir := filepath.Join(dir, ".git")
	fi, err := os.Stat(gitDir)
	if err != nil {
		return ""
	}
	if fi.IsDir() {
		return gitDir
	}
	raw, err := os.ReadFile(gitDir)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "gitdir:") {
		return ""
	}
	target := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	return filepath.Clean(target)
}
