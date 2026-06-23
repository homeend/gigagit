package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/rebaseplan"
)

// sameLineBranch builds main(initial) -> <names...> on "work", every commit
// REWRITING the single line of shared.txt to its own name. Because each commit
// touches the same line, replaying them out of their original order conflicts —
// the only way a squash hits the rebase-conflict path (an in-order/adjacent
// squash replays faithfully and never conflicts; see TestSquashAdjacentSameLineNeverConflicts).
func sameLineBranch(t *testing.T, names ...string) (string, *git.Repo) {
	t.Helper()
	dir, repo := newRepo(t)
	gitE(t, dir, "checkout", "-b", "work")
	for _, n := range names {
		os.WriteFile(filepath.Join(dir, "shared.txt"), []byte(n+"\n"), 0o644)
		gitE(t, dir, "add", ".")
		gitE(t, dir, "commit", "-m", n)
	}
	return dir, repo
}

// resolveAndFinishRebase drives the conflict → resolve → `git rebase --continue`
// loop a human would: while a rebase is in progress, overwrite the conflicted
// file with a fixed resolution, stage it, and continue. It caps the loop so a
// stuck rebase fails loudly instead of hanging. Content resolution is arbitrary
// — the point under test is that the squash's exec __rebase-message step still
// fires across each `--continue`, not which side wins.
func resolveAndFinishRebase(t *testing.T, dir string, repo *git.Repo) {
	t.Helper()
	for i := 0; i < 8; i++ {
		inProgress, err := repo.RebaseInProgress(context.Background(), "")
		if err != nil {
			t.Fatalf("rebase-in-progress probe: %v", err)
		}
		if !inProgress {
			return
		}
		os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("resolved\n"), 0o644)
		gitE(t, dir, "add", "-A")
		// --continue returns an error when it stops again on the NEXT conflict;
		// that is expected here — the loop re-probes and resolves again.
		_ = repo.RebaseContinue(context.Background(), "")
	}
	t.Fatal("rebase did not finish within 8 resolve+continue rounds")
}

// runReorderSquash builds the reorder-squash plan for targets (given as revs,
// newest-first) and runs InteractiveRebase, returning its result+error. onto is
// the oldest target's parent.
func runReorderSquash(t *testing.T, dir string, repo *git.Repo, gg string, targetRevs []string, deps OpDeps) (Result, error) {
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
	plan, err := rebaseplan.BuildSquashReorder(commits, targets)
	if err != nil {
		t.Fatalf("BuildSquashReorder: %v", err)
	}
	deps.Repo = repo
	return InteractiveRebase{Branch: "work", Onto: onto, Plan: plan, GGBin: gg}.Run(context.Background(), deps)
}

// messageContaining returns the first commit message in main..work whose body
// contains every wanted substring on its own line, or "" if none does.
func squashedMessage(t *testing.T, dir string, want ...string) string {
	t.Helper()
	full := gitOut(t, dir, "log", "--format=%B%x1e", "main..work")
	for _, msg := range strings.Split(full, "\x1e") {
		all := true
		for _, w := range want {
			if !lineEquals(msg, w) {
				all = false
				break
			}
		}
		if all && strings.TrimSpace(msg) != "" {
			return msg
		}
	}
	return ""
}

// lineEquals reports whether any line of msg equals want (trimmed) — exact-line
// match so single-char messages like "a" don't match incidentally inside words.
func lineEquals(msg, want string) bool {
	for _, ln := range strings.Split(msg, "\n") {
		if strings.TrimSpace(ln) == want {
			return true
		}
	}
	return false
}

// THE gap (known-bugs #3): a squash that conflicts, is resolved, and continued
// must still apply the concatenated message via the exec __rebase-message step.
// Selecting a & c (skipping b) replays c onto a out of order → the fixup
// conflicts → keep-conflicts pauses → resolve + continue → assert the combined
// message survived.
func TestSquashReorderConflictContinuePreservesMessage(t *testing.T) {
	gg := buildGG(t)
	dir, repo := sameLineBranch(t, "a", "b", "c") // work=c, work~1=b, work~2=a

	res, err := runReorderSquash(t, dir, repo, gg, []string{"work", "work~2"}, // c & a
		OpDeps{Decider: MapDecider{"rebase-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("expected a paused-on-conflict error from the reorder squash")
	}
	if !res.Changed || !strings.Contains(res.Summary, "paused") {
		t.Fatalf("result = %+v, want a paused summary", res)
	}

	resolveAndFinishRebase(t, dir, repo)

	if inProgress, _ := repo.RebaseInProgress(context.Background(), ""); inProgress {
		t.Fatal("rebase still in progress after resolve+continue")
	}
	if msg := squashedMessage(t, dir, "a", "c"); msg == "" {
		t.Fatalf("no commit carries the concatenated a+c message:\n%s",
			gitOut(t, dir, "log", "--format=%h %B", "main..work"))
	}
}

// Same path on a longer range with a target pair that excludes the oldest
// commit (b & d, skipping c), proving the exec-message-after-continue behaviour
// isn't specific to the oldest commit being a target or to a 3-commit range.
func TestSquashReorderConflictPreservesMessageMidRange(t *testing.T) {
	gg := buildGG(t)
	dir, repo := sameLineBranch(t, "a", "b", "c", "d") // work=d, work~1=c, work~2=b, work~3=a

	res, err := runReorderSquash(t, dir, repo, gg, []string{"work", "work~2"}, // d & b (skip c; a stays before)
		OpDeps{Decider: MapDecider{"rebase-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("expected a paused-on-conflict error")
	}
	if !res.Changed || !strings.Contains(res.Summary, "paused") {
		t.Fatalf("result = %+v, want a paused summary", res)
	}

	resolveAndFinishRebase(t, dir, repo)

	if inProgress, _ := repo.RebaseInProgress(context.Background(), ""); inProgress {
		t.Fatal("rebase still in progress after resolve+continue")
	}
	if msg := squashedMessage(t, dir, "b", "d"); msg == "" {
		t.Fatalf("no commit carries the concatenated b+d message:\n%s",
			gitOut(t, dir, "log", "--format=%h %B", "main..work"))
	}
}

// The abort fork through a squash conflict must leave the branch exactly as it
// was — original commits, no rebase in progress, nothing half-applied.
func TestSquashReorderConflictAbortRestoresBranch(t *testing.T) {
	gg := buildGG(t)
	dir, repo := sameLineBranch(t, "a", "b", "c")
	before := subjects(t, dir, "main..work") // [c b a]

	res, err := runReorderSquash(t, dir, repo, gg, []string{"work", "work~2"}, // c & a
		OpDeps{Decider: MapDecider{"rebase-conflict": "abort"}})
	if err != nil {
		t.Fatalf("abort path returned an error: %v", err)
	}
	if res.Changed {
		t.Fatalf("abort must report Changed=false, got %+v", res)
	}
	if inProgress, _ := repo.RebaseInProgress(context.Background(), ""); inProgress {
		t.Fatal("abort must leave no rebase in progress")
	}
	after := subjects(t, dir, "main..work")
	if strings.Join(after, "|") != strings.Join(before, "|") {
		t.Fatalf("branch changed after abort: before %v, after %v", before, after)
	}
}

// Counterpart that pins WHY the gap is reorder-only: an adjacent squash of
// same-line commits replays them in their original order, so it NEVER conflicts
// — the rebase-conflict decision is never reached (the decider records nothing)
// and the concatenated message lands without any --continue.
func TestSquashAdjacentSameLineNeverConflicts(t *testing.T) {
	gg := buildGG(t)
	dir, repo := sameLineBranch(t, "a", "b", "c") // work=c, work~1=b, work~2=a

	bSha := shaOf(t, dir, "work~1")
	cSha := shaOf(t, dir, "work")
	onto := bSha + "^"
	commits, err := repo.LogRangeMessages(context.Background(), onto, "work")
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	plan, err := rebaseplan.BuildSquash(commits, []string{cSha, bSha}) // adjacent b,c
	if err != nil {
		t.Fatalf("BuildSquash: %v", err)
	}
	dec := &captureDecider{answers: map[string]string{}}
	if _, err := (InteractiveRebase{Branch: "work", Onto: onto, Plan: plan, GGBin: gg}).
		Run(context.Background(), OpDeps{Repo: repo, Decider: dec}); err != nil {
		t.Fatalf("adjacent same-line squash should complete cleanly: %v", err)
	}
	if len(dec.seen) != 0 {
		t.Fatalf("adjacent squash must not hit the conflict decision, saw %+v", dec.seen)
	}
	if msg := squashedMessage(t, dir, "b", "c"); msg == "" {
		t.Fatalf("no commit carries the concatenated b+c message:\n%s",
			gitOut(t, dir, "log", "--format=%h %B", "main..work"))
	}
}
