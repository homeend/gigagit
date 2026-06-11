package git

import (
	"strings"

	"github.com/gigagit/gg/internal/model"
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
			cur = &model.Worktree{Path: strings.TrimPrefix(line, "worktree ")}
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
