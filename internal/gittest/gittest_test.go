package gittest

import (
	"os/exec"
	"strings"
	"testing"
)

func init() { Isolate() }

// The pinned global config must switch background maintenance OFF: with git
// ≥ 2.46 a fixture's `git commit` otherwise leaves a detached maintenance
// child holding .git/objects/maintenance.lock, which TemplateRepo's copy
// lists and then fails to open (a CI-only flake; see Isolate).
func TestIsolateDisablesBackgroundMaintenance(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]string{"maintenance.auto": "false", "gc.auto": "0"} {
		out, err := exec.Command("git", "config", "--global", "--get", key).Output()
		if err != nil {
			t.Fatalf("git config --global --get %s: %v", key, err)
		}
		if got := strings.TrimSpace(string(out)); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}
