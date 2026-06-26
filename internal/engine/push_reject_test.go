package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// rejectingPushRepo returns a fake whose first "git push" fails non-fast-forward.
func rejectingPushRepo() (*git.Repo, *gitexec.FakeRunner) {
	f := gitexec.NewFakeRunner()
	f.SetError("git push", errors.New(
		"git push failed (exit 1): ! [rejected] main -> main (non-fast-forward)"))
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
