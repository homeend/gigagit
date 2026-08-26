package web

import "testing"

// The bug this guards is Windows-only and invisible on Linux: git reports a
// top-level with forward slashes even on Windows, while the MRU registry
// stores filepath.Clean'd paths (backslashes there). The comparison is
// written against an injected GOOS precisely so the Windows rules can be
// exercised from any test run.
func TestSameRepoPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		goos string
		a, b string
		want bool
	}{
		{"identical posix", "linux", "/home/u/repo", "/home/u/repo", true},
		{"different posix", "linux", "/home/u/repo", "/home/u/other", false},
		{"trailing slash", "linux", "/home/u/repo/", "/home/u/repo", true},
		{"dot segment", "linux", "/home/u/./repo", "/home/u/repo", true},
		{"root stays root", "linux", "/", "/", true},

		// The real-world pair: `rev-parse --show-toplevel` vs repos.Touch.
		{"windows separators differ", "windows", `T:\others\repo`, "T:/others/repo", true},
		{"windows case differs", "windows", `T:\Others\Repo`, "t:/others/repo", true},
		{"windows genuinely different", "windows", `T:\others\repo`, "T:/others/other", false},

		// A case-only difference is NOT the same path on Linux.
		{"posix case matters", "linux", "/home/u/Repo", "/home/u/repo", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The GOOS is a parameter, not a package-var override: a pathGOOS
			// write here raced with other parallel tests' live servers.
			if got := sameRepoPathOn(c.goos, c.a, c.b); got != c.want {
				t.Errorf("sameRepoPath(%q, %q) on %s = %v, want %v", c.a, c.b, c.goos, got, c.want)
			}
		})
	}
}

// Separator normalization must not depend on the OS running the test, or the
// Windows cases above would only be meaningful on Windows.
func TestNormalizeRepoPathIsOSIndependent(t *testing.T) {
	t.Parallel()
	if got := normalizeRepoPath(`T:\a\b\`); got != "T:/a/b" {
		t.Errorf("normalizeRepoPath = %q, want T:/a/b", got)
	}
	if got := normalizeRepoPath("/a/b/../c"); got != "/a/c" {
		t.Errorf("normalizeRepoPath = %q, want /a/c", got)
	}
}
