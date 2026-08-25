package git

import "testing"

func TestVersionRefRoundTrip(t *testing.T) {
	t.Parallel()
	ref := VersionRef("feat/x/y", "delete-branch", 1753100000)
	if ref != "refs/gg/versions/feat/x/y/1753100000-delete-branch" {
		t.Fatalf("ref = %q", ref)
	}
	b, op, ts, ok := ParseVersionRef(ref)
	if !ok || b != "feat/x/y" || op != "delete-branch" || ts != 1753100000 {
		t.Fatalf("parse = %q %q %d %v", b, op, ts, ok)
	}
}

func TestParseVersionRefRejects(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"refs/heads/main",
		"refs/gg/versions/",
		"refs/gg/versions/main",          // no <ts>-<op> segment
		"refs/gg/versions/main/x-rebase", // non-numeric ts
	} {
		if _, _, _, ok := ParseVersionRef(bad); ok {
			t.Fatalf("ParseVersionRef(%q) unexpectedly ok", bad)
		}
	}
}
