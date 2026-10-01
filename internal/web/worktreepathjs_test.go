package web

import (
	"strings"
	"testing"
)

// The top bar's worktree path (top right) copies: a double-click copies it
// whole, a right-click offers the copy as a menu row.
func TestWorktreePathCopyIsWired(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want, why string }{
		{"ops.js", `$("repo-worktree").addEventListener("dblclick"`, "a double-click copies the path"},
		{"ops.js", `$("repo-worktree").addEventListener("contextmenu"`, "a right-click opens the copy menu"},
		{"ops.js", `label: "copy path"`, "the menu row says what it copies"},
		{"ops.js", `copyText(state.worktree, "path")`, "both copy the served worktree path"},
		{"index.html", `<span id="repo-worktree" title="double-click to copy the path · right-click for the menu">`, "the label says it copies"},
		{"index.html", `<span class="hkey">worktree path</span>`, "the help overlay advertises it"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
