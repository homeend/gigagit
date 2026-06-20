package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureDecider records the requests it sees and answers from a fixed map.
type captureDecider struct {
	answers map[string]string
	seen    []DecisionRequest
}

func (d *captureDecider) Decide(_ context.Context, req DecisionRequest) (DecisionResponse, error) {
	d.seen = append(d.seen, req)
	return DecisionResponse{Option: d.answers[req.ID]}, nil
}

// The reset-mode options must lead with a SAFE mode so the modal's default
// cursor (index 0) is never the destructive "hard" — enter must not be one
// keystroke from discarding work.
func TestResetModeOptionsLeadWithSafe(t *testing.T) {
	dir, repo, base := resetEngineRepo(t)
	_ = dir
	dec := &captureDecider{answers: map[string]string{"reset-mode": "cancel"}}
	if _, err := (Reset{Commit: base}).Run(context.Background(), OpDeps{Repo: repo, Decider: dec}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(dec.seen) == 0 || dec.seen[0].ID != "reset-mode" {
		t.Fatalf("first decision = %+v, want reset-mode", dec.seen)
	}
	opts := dec.seen[0].Options
	if len(opts) == 0 || opts[0] == "hard" {
		t.Fatalf("reset-mode options must not lead with hard: %v", opts)
	}
	if opts[0] != "soft" {
		t.Fatalf("reset-mode options[0] = %q, want soft (safe default)", opts[0])
	}
}

func TestResetGuardEmptyCommit(t *testing.T) {
	_, repo := newRepo(t)
	_, err := Reset{}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "Commit is required") {
		t.Fatalf("err = %v, want 'Commit is required'", err)
	}
}

// resetEngineRepo: base (returned SHA) then a second commit adding b.txt; HEAD on
// the second. The target base is an ancestor of HEAD (the common backward case).
func resetEngineRepo(t *testing.T) (string, GitOps, string) {
	t.Helper()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add b.txt")
	return dir, repo, base
}

func TestResetCancelDoesNothing(t *testing.T) {
	dir, repo, base := resetEngineRepo(t)
	before := gitOut(t, dir, "rev-parse", "HEAD")
	res, err := Reset{Commit: base}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"reset-mode": "cancel"}})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if res.Changed || !strings.Contains(res.Summary, "cancelled") {
		t.Fatalf("result = %+v", res)
	}
	if gitOut(t, dir, "rev-parse", "HEAD") != before {
		t.Fatal("cancel must not move HEAD")
	}
}

func TestResetSoftMode(t *testing.T) {
	dir, repo, base := resetEngineRepo(t)
	res, err := Reset{Commit: base}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"reset-mode": "soft"}})
	if err != nil {
		t.Fatalf("soft: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "soft") {
		t.Fatalf("result = %+v", res)
	}
	if gitOut(t, dir, "rev-parse", "HEAD") != base {
		t.Fatal("HEAD should be at base")
	}
}

func TestResetHardMode(t *testing.T) {
	dir, repo, base := resetEngineRepo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dirty\n"), 0o644) // tracked edit
	res, err := Reset{Commit: base}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"reset-mode": "hard"}})
	if err != nil {
		t.Fatalf("hard: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("hard reset should remove b.txt")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "base\n" {
		t.Fatalf("a.txt = %q, hard reset should discard the dirty edit", got)
	}
}

// Resetting to a commit on another branch (NOT an ancestor of HEAD) requires the
// reset-confirm decision; cancel leaves the branch untouched.
func TestResetNonAncestorConfirmCancel(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "base")
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feat\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "feat change")
	featTip := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")
	mainTip := gitOut(t, dir, "rev-parse", "HEAD")

	// cancel at the confirm
	res, err := Reset{Commit: featTip}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"reset-mode": "mixed", "reset-confirm": "cancel"}})
	if err != nil {
		t.Fatalf("confirm cancel: %v", err)
	}
	if res.Changed || !strings.Contains(res.Summary, "cancelled") {
		t.Fatalf("result = %+v", res)
	}
	if gitOut(t, dir, "rev-parse", "HEAD") != mainTip {
		t.Fatal("a cancelled non-ancestor reset must not move the branch")
	}

	// proceed at the confirm → branch moves onto feat's tip
	res, err = Reset{Commit: featTip}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"reset-mode": "mixed", "reset-confirm": "proceed"}})
	if err != nil {
		t.Fatalf("confirm proceed: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	if gitOut(t, dir, "rev-parse", "HEAD") != featTip {
		t.Fatal("a confirmed non-ancestor reset must move the branch onto the target")
	}
}

// A backward reset along the current branch (target IS an ancestor) skips the
// confirm — only the mode decision is consulted.
func TestResetAncestorSkipsConfirm(t *testing.T) {
	dir, repo, base := resetEngineRepo(t)
	res, err := Reset{Commit: base}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"reset-mode": "mixed"}}) // no reset-confirm provided
	if err != nil {
		t.Fatalf("ancestor reset must not need a confirm: %v", err)
	}
	if !res.Changed || gitOut(t, dir, "rev-parse", "HEAD") != base {
		t.Fatalf("result = %+v / HEAD not at base", res)
	}
}
