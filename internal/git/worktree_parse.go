package git

import (
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// ParseWorktrees parses `git worktree list --porcelain` output. Records are
// separated by blank lines; each record has worktree/HEAD/branch|detached|bare.
func ParseWorktrees(data []byte) ([]model.Worktree, error) {
	var out []model.Worktree
	var cur *model.Worktree
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			// git prints forward slashes even on Windows; Clean converts to
			// the platform's native notation so every consumer can compare
			// this path against filepath-built ones (remove/move matching,
			// current-worktree markers) without / vs \ mismatches.
			cur = &model.Worktree{Path: filepath.Clean(strings.TrimPrefix(line, "worktree "))}
		case cur == nil:
			// ignore stray lines
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "bare":
			cur.Bare = true
		}
	}
	flush()
	return out, nil
}
