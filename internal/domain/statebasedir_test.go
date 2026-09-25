package domain

import (
	"path/filepath"
	"testing"
)

// TestStateBaseDirKinds pins every kind string stateBaseDir's eight (now
// nine) callers pass it. This is the test that would have caught the
// original breach — a changed segment silently orphans every existing
// user's stored data under $XDG_STATE_HOME, with no error.
func TestStateBaseDirKinds(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	kinds := []string{
		"bookmark",
		"notes",
		"prefix",
		"previews",
		"profile",
		"reviews",
		"search",
		"shelf",
		"linkhist",
		"tasks",
	}
	for _, kind := range kinds {
		want := filepath.Join(stateHome, "gg", kind)
		if got := stateBaseDir(kind); got != want {
			t.Errorf("stateBaseDir(%q) = %q, want %q", kind, got, want)
		}
	}
}
