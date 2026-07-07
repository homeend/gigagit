package tui

import (
	"path/filepath"
	"testing"
)

func TestRepoRelPath(t *testing.T) {
	root := filepath.FromSlash("/repo")
	outside := filepath.FromSlash("/elsewhere/x.go")
	cases := []struct{ name, in, want string }{
		{"already relative", "internal/tui/model.go", "internal/tui/model.go"},
		{"dot-slash relative", "./internal/x.go", "internal/x.go"},
		{"absolute inside repo", filepath.FromSlash("/repo/internal/x.go"), "internal/x.go"},
		{"absolute outside repo", outside, filepath.ToSlash(filepath.Clean(outside))},
		{"blank", "   ", ""},
	}
	for _, c := range cases {
		if got := repoRelPath(root, c.in); got != c.want {
			t.Errorf("%s: repoRelPath(%q,%q)=%q want %q", c.name, root, c.in, got, c.want)
		}
	}
}
