package tui

import "testing"

func TestRestoredPath(t *testing.T) {
	cases := map[string]string{
		"config.go":     "config_RESTORED.go",
		"a/b/config.go": "a/b/config_RESTORED.go",
		"Makefile":      "Makefile_RESTORED",
		"a/b/Makefile":  "a/b/Makefile_RESTORED",
		".gitignore":    ".gitignore_RESTORED",
		"a/.gitignore":  "a/.gitignore_RESTORED",
		".env.local":    ".env.local_RESTORED",
		"":              "_RESTORED",
	}
	for in, want := range cases {
		if got := restoredPath(in); got != want {
			t.Errorf("restoredPath(%q) = %q, want %q", in, got, want)
		}
	}
}
