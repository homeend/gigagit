package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/rebaseplan"
)

// runSquash drives the full squash path the TUI uses: derive Onto = oldest^,
// load the range, build the squash plan, run InteractiveRebase to completion.
// targetRevs are given newest-first (e.g. "work", "work~1").
func runSquash(t *testing.T, dir string, repo *git.Repo, gg string, targetRevs []string, deps OpDeps) (Result, error) {
	t.Helper()
	var targets []string
	for _, rv := range targetRevs {
		targets = append(targets, shaOf(t, dir, rv))
	}
	oldest := targets[len(targets)-1] // newest-first → last is oldest
	onto := oldest + "^"
	commits, err := repo.LogRangeMessages(context.Background(), onto, "work")
	if err != nil {
		t.Fatalf("range %s..work: %v", onto, err)
	}
	plan, err := rebaseplan.BuildSquash(commits, targets)
	if err != nil {
		t.Fatalf("BuildSquash: %v", err)
	}
	deps.Repo = repo
	return InteractiveRebase{Branch: "work", Onto: onto, Plan: plan, GGBin: gg}.Run(context.Background(), deps)
}

// Ground truth: squashing two adjacent commits actually collapses them into one
// real commit (no editor, no hang) with the concatenated message.
func TestSquashTwoCommitsEndToEnd(t *testing.T) {
	gg := buildGG(t)
	dir, repo := fourCommitBranch(t) // main -> a -> b -> c -> d
	if _, err := runSquash(t, dir, repo, gg, []string{"work", "work~1"}, OpDeps{}); err != nil {
		t.Fatalf("squash d into c: %v", err)
	}
	got := subjects(t, dir, "main..work") // newest-first
	if want := []string{"c", "b", "a"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("after squash d into c: %v, want %v", got, want)
	}
	// The surviving commit's message concatenates c and d.
	msg := gitOut(t, dir, "log", "-1", "--format=%B", "work")
	if !strings.Contains(msg, "c") || !strings.Contains(msg, "d") {
		t.Fatalf("squashed message = %q, want both c and d", msg)
	}
}

func TestSquashThreeCommitsEndToEnd(t *testing.T) {
	gg := buildGG(t)
	dir, repo := fourCommitBranch(t)
	// Squash b, c, d (the three newest after a) into one.
	if _, err := runSquash(t, dir, repo, gg, []string{"work", "work~1", "work~2"}, OpDeps{}); err != nil {
		t.Fatalf("squash c,d into b: %v", err)
	}
	got := subjects(t, dir, "main..work")
	if want := []string{"b", "a"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("after squash: %v, want %v", got, want)
	}
}
