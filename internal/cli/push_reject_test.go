package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPushRejectPolicy(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]string{
		"rebase":           {"push-rejected": "rebase", "rebase-conflict": "keep-conflicts"},
		"force":            {"push-rejected": "force", "push-force": "force"},
		"force-with-lease": {"push-rejected": "force", "push-force": "force-with-lease"},
		"abort":            {"push-rejected": "abort"},
		// Unset leaves push-rejected unanswered: a non-interactive rejected push
		// then fails fast (cliDecider errors); an interactive one prompts.
		"": {},
	}
	for in, want := range cases {
		got, err := pushRejectPolicy(in)
		if err != nil {
			t.Fatalf("pushRejectPolicy(%q) err=%v", in, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("pushRejectPolicy(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPushRejectPolicyRejectsUnknown(t *testing.T) {
	t.Parallel()
	if _, err := pushRejectPolicy("merge"); err == nil {
		t.Fatal("unknown --on-reject value must error")
	}
}

// cloneConflictDiverged builds a clone whose local commit edits the SAME line
// the remote advanced, so `--on-reject=rebase` hits a real rebase conflict (not
// a MapDecider-injected one).
func cloneConflictDiverged(t *testing.T) (clone, origin string) {
	t.Helper()
	root := t.TempDir()
	origin = filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone = filepath.Join(root, "clone")
	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v1")
	gitIn(t, seed, "push", "-u", "origin", "main")

	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	gitIn(t, clone, "config", "user.name", "t")
	gitIn(t, clone, "config", "user.email", "t@t")

	// Origin advances: f.txt -> v2 (the remote moved).
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v2")
	gitIn(t, seed, "push", "origin", "main")

	// Clone edits the same line differently → rebase will conflict.
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("mine\n"), 0o644)
	gitIn(t, clone, "add", ".")
	gitIn(t, clone, "commit", "-m", "local edit")
	return clone, origin
}

// TestPushOnRejectRebaseConflictFailsCleanly drives a real conflicting rebase
// through the CLI: the recovery must exit non-zero (the push did not happen),
// report the conflict, and leave origin unchanged — never dead-end on an
// unanswerable rebase-conflict decision.
func TestPushOnRejectRebaseConflictFailsCleanly(t *testing.T) {
	t.Parallel()
	clone, origin := cloneConflictDiverged(t)
	code, _, stderr := runCLI(t, clone, "push", "--on-reject=rebase")
	if code == 0 {
		t.Fatal("a conflicting rebase recovery must exit non-zero")
	}
	if !strings.Contains(strings.ToLower(stderr), "conflict") {
		t.Fatalf("stderr should report the conflict, got %q", stderr)
	}
	if got := originContent(t, origin); got != "v2\n" {
		t.Fatalf("origin content = %q, want unchanged v2 after a failed recovery", got)
	}
}
