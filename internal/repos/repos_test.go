package repos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tmpState returns a state-file path inside a fresh temp dir (file not created).
func tmpState(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "sub", "repos.toml") // parent missing on purpose
}

func TestTouchCreatesAndLoadIsMRUFirst(t *testing.T) {
	state := tmpState(t)
	a, b := t.TempDir(), t.TempDir()
	if err := Touch(state, a, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := Touch(state, b, time.Unix(2000, 0)); err != nil {
		t.Fatal(err)
	}
	got := Load(state)
	if len(got) != 2 || got[0].Path != b || got[1].Path != a {
		t.Fatalf("MRU order wrong: %+v", got)
	}
}

func TestTouchDedupesAndBumps(t *testing.T) {
	state := tmpState(t)
	a, b := t.TempDir(), t.TempDir()
	_ = Touch(state, a, time.Unix(1000, 0))
	_ = Touch(state, b, time.Unix(2000, 0))
	if err := Touch(state, a, time.Unix(3000, 0)); err != nil {
		t.Fatal(err)
	}
	got := Load(state)
	if len(got) != 2 {
		t.Fatalf("dedupe failed: %+v", got)
	}
	if got[0].Path != a || !got[0].LastOpened.Equal(time.Unix(3000, 0)) {
		t.Fatalf("bump failed: %+v", got[0])
	}
}

func TestLoadPrunesDeadPaths(t *testing.T) {
	state := tmpState(t)
	alive := t.TempDir()
	dead := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(dead, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = Touch(state, alive, time.Unix(1000, 0))
	_ = Touch(state, dead, time.Unix(2000, 0))
	if err := os.RemoveAll(dead); err != nil {
		t.Fatal(err)
	}
	got := Load(state)
	if len(got) != 1 || got[0].Path != alive {
		t.Fatalf("dead path not pruned: %+v", got)
	}
}

func TestRemoveForgetsEntry(t *testing.T) {
	state := tmpState(t)
	a, b := t.TempDir(), t.TempDir()
	_ = Touch(state, a, time.Unix(1000, 0))
	_ = Touch(state, b, time.Unix(2000, 0))
	if err := Remove(state, a); err != nil {
		t.Fatal(err)
	}
	got := Load(state)
	if len(got) != 1 || got[0].Path != b {
		t.Fatalf("remove failed: %+v", got)
	}
	// Removing an absent path is not an error.
	if err := Remove(state, filepath.Join(t.TempDir(), "never")); err != nil {
		t.Fatalf("remove of absent entry should be nil, got %v", err)
	}
}

func TestCorruptStateActsEmpty(t *testing.T) {
	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := os.WriteFile(state, []byte("not [valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Load(state); len(got) != 0 {
		t.Fatalf("corrupt state should act empty, got %+v", got)
	}
	// And the next Touch rewrites it whole.
	a := t.TempDir()
	if err := Touch(state, a, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if got := Load(state); len(got) != 1 || got[0].Path != a {
		t.Fatalf("touch after corruption failed: %+v", got)
	}
}

func TestEmptyStatePathDisablesRecording(t *testing.T) {
	if err := Touch("", t.TempDir(), time.Now()); err != nil {
		t.Fatalf("empty state path must be a silent no-op, got %v", err)
	}
	if got := Load(""); len(got) != 0 {
		t.Fatalf("Load(\"\") should be empty, got %+v", got)
	}
	if err := Remove("", "/x"); err != nil {
		t.Fatalf("Remove with empty path must be nil, got %v", err)
	}
}

func TestNoTempLitterAfterWrites(t *testing.T) {
	state := tmpState(t)
	_ = Touch(state, t.TempDir(), time.Unix(1000, 0))
	entries, err := os.ReadDir(filepath.Dir(state))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "repos-") && e.Name() != "repos.toml" {
			t.Fatalf("temp litter left behind: %s", e.Name())
		}
	}
}

func TestNameIsBase(t *testing.T) {
	if got := Name(Entry{Path: "/a/b/mono"}); got != "mono" {
		t.Fatalf("Name = %q, want mono", got)
	}
}
