package web

import (
	"strings"
	"testing"
)

// A TUI-hosted page hides its four repo-switch affordances; the strings
// below are the wiring pins (the logic is one `state.hosted` gate each).
func TestHostedPageHidesRepoSwitching(t *testing.T) {
	t.Parallel()
	cases := []struct{ file, want string }{
		{"core.js", "hosted: false,"},
		{"ops.js", "state.hosted = !!repo.hosted;"},
		{"palette.js", `state.hosted && (r.label === "switch repo…" || r.label === "open repo (path)…")`},
		{"palette.js", `...(state.hosted ? [] : [{ header: "Repositories" }, { label: "switch repo…", act: () => openPalette("repo") }]),`},
		{"sidebar.js", "if (!state.hosted && !(state.worktree && w.path === state.worktree))"},
		{"locks.js", "if (served && !state.hosted) doReroot(to);"},
	}
	for _, c := range cases {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s: missing %q", c.file, c.want)
		}
	}
}
