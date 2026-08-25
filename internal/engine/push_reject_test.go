package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// rejectingPushRepo returns a fake whose first "git push" fails non-fast-forward.
// HEAD reports "main" so a push of "main" (the common case) sees its target as the
// current branch and the rebase recovery stays on offer.
func rejectingPushRepo() (*git.Repo, *gitexec.FakeRunner) {
	f := gitexec.NewFakeRunner()
	f.SetError("git push", errors.New(
		"git push failed (exit 1): ! [rejected] main -> main (non-fast-forward)"))
	f.SetResponse("git symbolic-ref", gitexec.Result{Stdout: "main\n"})
	return &git.Repo{Runner: f}, f
}

func pushCallCount(f *gitexec.FakeRunner) int {
	n := 0
	for _, c := range f.Calls {
		if c.Name == "git push" {
			n++
		}
	}
	return n
}

// lastPushArgv returns the argv of the final "git push" call.
func lastPushArgv(f *gitexec.FakeRunner) (argv []string, ok bool) {
	for _, c := range f.Calls {
		if c.Name == "git push" {
			argv, ok = c.Argv, true
		}
	}
	return argv, ok
}

// forceSucceedAfter returns a handler that fails the first n "git push" calls
// non-fast-forward and succeeds thereafter. SetHandler takes precedence over
// SetError for the same span, so it overrides rejectingPushRepo's error.
func forceSucceedAfter(n int, f *gitexec.FakeRunner) func(context.Context, []string) (gitexec.Result, error) {
	return func(_ context.Context, _ []string) (gitexec.Result, error) {
		// pushCallCount already counts THIS call (RunEnv records before dispatch).
		if pushCallCount(f) <= n {
			return gitexec.Result{}, errors.New(
				"git push failed (exit 1): ! [rejected] main -> main (non-fast-forward)")
		}
		return gitexec.Result{}, nil
	}
}

func TestPushRejectedAbortDoesNotForce(t *testing.T) {
	t.Parallel()
	repo, f := rejectingPushRepo()
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{"push-rejected": "abort"}})
	if err != nil {
		t.Fatalf("abort should be a clean no-op, err=%v", err)
	}
	if res.Changed {
		t.Fatal("abort must not report a change")
	}
	if pushCallCount(f) != 1 {
		t.Fatalf("abort must not push again, push calls=%d", pushCallCount(f))
	}
}

func TestPushRejectedForceChainsForceDecision(t *testing.T) {
	t.Parallel()
	repo, f := rejectingPushRepo()
	f.SetHandler("git push", forceSucceedAfter(1, f)) // 1st rejected, 2nd+ succeed
	dec := &captureDecider{answers: map[string]string{
		"push-rejected": "force",
		"push-force":    "force",
	}}
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: dec})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(dec.seen) < 2 || dec.seen[0].ID != "push-rejected" || dec.seen[1].ID != "push-force" {
		t.Fatalf("decision order = %+v, want [push-rejected, push-force]", dec.seen)
	}
	argv, _ := lastPushArgv(f)
	if !hasArg(argv, "--force") {
		t.Fatalf("force branch must push with --force, got %v", argv)
	}
}

func TestPushRejectedNeverFiresOnHookRejection(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetError("git push", errors.New(
		"git push failed (exit 1): ! [remote rejected] main -> main (pre-receive hook declined)"))
	dec := &captureDecider{answers: map[string]string{}}
	_, err := Push{Remote: "origin", Branch: "main"}.Run(
		context.Background(), OpDeps{Repo: &git.Repo{Runner: f}, Decider: dec})
	if err == nil {
		t.Fatal("a hook rejection must surface as an error, not a recovery")
	}
	if len(dec.seen) != 0 {
		t.Fatalf("no decision may be raised for a hook rejection, saw %+v", dec.seen)
	}
}

func TestPushRejectedRebaseThenPush(t *testing.T) {
	t.Parallel()
	repo, f := rejectingPushRepo()
	f.SetResponse("git pull", gitexec.Result{})       // clean rebase
	f.SetHandler("git push", forceSucceedAfter(1, f)) // 1st push rejected, 2nd succeeds
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{"push-rejected": "rebase"}})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if pushCallCount(f) != 2 {
		t.Fatalf("want exactly 2 pushes (rejected, then re-push), got %d", pushCallCount(f))
	}
	pulls := 0
	for _, c := range f.Calls {
		if c.Name == "git pull" {
			pulls++
			if !hasArg(c.Argv, "--rebase") {
				t.Fatalf("recovery pull must be --rebase, got %v", c.Argv)
			}
		}
	}
	if pulls != 1 {
		t.Fatalf("want one pull --rebase, got %d", pulls)
	}
}

func TestPushRejectedRebaseConflictKeepLeavesTreeAndErrors(t *testing.T) {
	t.Parallel()
	repo, f := rejectingPushRepo()
	f.SetError("git pull", errors.New("git pull failed: CONFLICT (content)"))
	f.SetResponse("git rebase --show-current-patch", gitexec.Result{}) // exit 0 ⇒ rebase in progress
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{
			"push-rejected":   "rebase",
			"rebase-conflict": "keep-conflicts",
		}})
	if err == nil {
		t.Fatal("a kept conflict must return an error so the TUI conflict process engages")
	}
	if !res.Changed {
		t.Fatal("a kept conflict leaves a changed (conflicted) tree")
	}
	if pushCallCount(f) != 1 {
		t.Fatalf("a kept conflict must not re-push, push calls=%d", pushCallCount(f))
	}
}

func TestPushRejectedRebaseConflictAbortRunsRebaseAbort(t *testing.T) {
	t.Parallel()
	repo, f := rejectingPushRepo()
	f.SetError("git pull", errors.New("git pull failed: CONFLICT (content)"))
	f.SetResponse("git rebase --show-current-patch", gitexec.Result{})
	f.SetResponse("git rebase --abort", gitexec.Result{})
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{
			"push-rejected":   "rebase",
			"rebase-conflict": "abort",
		}})
	if err != nil {
		t.Fatalf("abort of a conflicted rebase is a clean no-op, err=%v", err)
	}
	if res.Changed {
		t.Fatal("abort must not report a change")
	}
	saw := false
	for _, c := range f.Calls {
		if c.Name == "git rebase --abort" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("abort branch must call git rebase --abort")
	}
}

// pushRejectedOptions runs a rejected push and returns the option list offered
// by the push-rejected decision.
func pushRejectedOptions(t *testing.T, f *gitexec.FakeRunner, branch string) []string {
	t.Helper()
	dec := &captureDecider{answers: map[string]string{"push-rejected": "abort"}}
	if _, err := (Push{Remote: "origin", Branch: branch, SetUpstream: true}).Run(
		context.Background(), OpDeps{Repo: &git.Repo{Runner: f}, Decider: dec}); err != nil {
		t.Fatalf("abort is a clean no-op, err=%v", err)
	}
	for _, d := range dec.seen {
		if d.ID == "push-rejected" {
			return d.Options
		}
	}
	t.Fatal("push-rejected decision was never raised")
	return nil
}

// TestPushRejectedNonCurrentBranchOmitsRebase: the rebase recovery rebases the
// CURRENT HEAD, so it is invalid when the rejected push targets a different
// branch (the Branches-panel "Push <branch>" action). Such a push must offer
// force/abort only — never rebase, which would rewrite the wrong branch — and
// must not run any pull.
func TestPushRejectedNonCurrentBranchOmitsRebase(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetError("git push", errors.New(
		"git push failed (exit 1): ! [rejected] feature -> feature (non-fast-forward)"))
	f.SetResponse("git symbolic-ref", gitexec.Result{Stdout: "main\n"}) // HEAD = main, not feature
	f.SetResponse("git pull", gitexec.Result{})                         // any pull here is a bug

	opts := pushRejectedOptions(t, f, "feature")
	for _, o := range opts {
		if o == "rebase" {
			t.Fatalf("rebase must NOT be offered pushing a non-current branch, opts=%v", opts)
		}
	}
	for _, c := range f.Calls {
		if c.Name == "git pull" {
			t.Fatal("no pull may run when pushing a non-current branch")
		}
	}
}

// TestPushRejectedCurrentBranchKeepsRebase pins that the common case (pushing the
// checked-out branch) still offers rebase, so this guard didn't regress it.
func TestPushRejectedCurrentBranchKeepsRebase(t *testing.T) {
	t.Parallel()
	_, f := rejectingPushRepo() // HEAD = main
	opts := pushRejectedOptions(t, f, "main")
	saw := false
	for _, o := range opts {
		if o == "rebase" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("rebase must stay on offer pushing the current branch, opts=%v", opts)
	}
}

// TestPushRejectedNonCurrentBranchRebaseGuarded is defensive: even if a decider
// answers "rebase" for a non-current branch (off the offered list), the op must
// refuse rather than rebase the wrong branch.
func TestPushRejectedNonCurrentBranchRebaseGuarded(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetError("git push", errors.New(
		"git push failed (exit 1): ! [rejected] feature -> feature (non-fast-forward)"))
	f.SetResponse("git symbolic-ref", gitexec.Result{Stdout: "main\n"})
	f.SetResponse("git pull", gitexec.Result{})
	_, err := (Push{Remote: "origin", Branch: "feature", SetUpstream: true}).Run(
		context.Background(), OpDeps{Repo: &git.Repo{Runner: f}, Decider: MapDecider{"push-rejected": "rebase"}})
	if err == nil {
		t.Fatal("rebasing a non-current branch must surface an error, not silently rebase HEAD")
	}
	for _, c := range f.Calls {
		if c.Name == "git pull" {
			t.Fatal("the guard must prevent any pull --rebase on a non-current branch")
		}
	}
}

func TestPushRejectedRebaseSecondRejectionDoesNotLoop(t *testing.T) {
	t.Parallel()
	repo, f := rejectingPushRepo() // every "git push" stays rejected
	f.SetResponse("git pull", gitexec.Result{})
	dec := &captureDecider{answers: map[string]string{"push-rejected": "rebase"}}
	_, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: dec})
	if err == nil {
		t.Fatal("a second rejection after rebase must surface as an error")
	}
	rejectAsks := 0
	for _, d := range dec.seen {
		if d.ID == "push-rejected" {
			rejectAsks++
		}
	}
	if rejectAsks != 1 {
		t.Fatalf("push-rejected asked %d times, want 1 (no loop)", rejectAsks)
	}
}
