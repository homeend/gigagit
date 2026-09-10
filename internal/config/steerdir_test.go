package config

import (
	"path/filepath"
	"testing"
)

// The steer inbox is keyed by WORKTREE inside the repo's session dir: the
// snapshot is shared by every worktree of a repo, but a `gg session navigate`
// run in worktree A must reach the TUI showing A.
func TestSessionSteerDirIsPerWorktreeUnderTheSessionDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // no t.Parallel: this test sets an env var
	common := filepath.Join(string(filepath.Separator), "repo", ".git")
	a := SessionSteerDir(common, filepath.Join(string(filepath.Separator), "repo"))
	b := SessionSteerDir(common, filepath.Join(string(filepath.Separator), "wt", "feat"))
	if a == "" || b == "" {
		t.Fatalf("SessionSteerDir = %q / %q, want real paths", a, b)
	}
	if a == b {
		t.Fatalf("two worktrees of one repo share the inbox %q", a)
	}
	snapDir := filepath.Dir(SessionSnapshotPath(common))
	for _, got := range []string{a, b} {
		if filepath.Dir(filepath.Dir(got)) != snapDir {
			t.Errorf("SessionSteerDir = %q, want <sessionDir>/steer/<worktree key> under %q", got, snapDir)
		}
		if filepath.Base(filepath.Dir(got)) != "steer" {
			t.Errorf("SessionSteerDir = %q, want its parent named \"steer\"", got)
		}
	}
	if filepath.Base(a) != EncodeRepoKey(filepath.Join(string(filepath.Separator), "repo")) {
		t.Errorf("leaf = %q, want EncodeRepoKey of the worktree", filepath.Base(a))
	}
}

func TestSessionSteerDirEmptyArgs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if got := SessionSteerDir("", "/repo"); got != "" {
		t.Errorf("no common dir: got %q, want \"\" (steering off)", got)
	}
	if got := SessionSteerDir("/repo/.git", ""); got != "" {
		t.Errorf("no worktree: got %q, want \"\" (steering off)", got)
	}
}

func TestSteeringOnDefaultsToOn(t *testing.T) {
	t.Parallel()
	if !Defaults().UI.SteeringOn() {
		t.Error("agent steering must be ON by default")
	}
	if got := Defaults().UI.AgentSteering; got != "on" {
		t.Errorf("default agent_steering = %q, want \"on\"", got)
	}
	if (UIConfig{AgentSteering: "off"}).SteeringOn() {
		t.Error(`agent_steering = "off" must turn steering off`)
	}
	if !(UIConfig{}).SteeringOn() {
		t.Error("an unset agent_steering must read as on (zero-is-unset)")
	}
}
