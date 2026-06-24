package engine

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

func pushFakeRepo() (*git.Repo, *gitexec.FakeRunner) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git push", gitexec.Result{})
	return &git.Repo{Runner: f}, f
}

// pushArgv returns the argv of the single "git push" call, or ok=false if push
// was never invoked.
func pushArgv(f *gitexec.FakeRunner) (argv []string, ok bool) {
	for _, c := range f.Calls {
		if c.Name == "git push" {
			return c.Argv, true
		}
	}
	return nil, false
}

func hasArg(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}

func TestPushNoForceSkipsDecisionAndForceFlag(t *testing.T) {
	repo, f := pushFakeRepo()
	dec := &captureDecider{answers: map[string]string{}}
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: dec})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(dec.seen) != 0 {
		t.Fatalf("plain push must ask no decision, saw %+v", dec.seen)
	}
	argv, ok := pushArgv(f)
	if !ok {
		t.Fatal("push not called")
	}
	if hasArg(argv, "--force") || hasArg(argv, "--force-with-lease") {
		t.Fatalf("plain push carried a force flag: %v", argv)
	}
}

func TestPushForceWithLeaseEmitsLeaseFlag(t *testing.T) {
	repo, f := pushFakeRepo()
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true, Force: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{"push-force": "force-with-lease"}})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	argv, ok := pushArgv(f)
	if !ok || !hasArg(argv, "--force-with-lease") || hasArg(argv, "--force") {
		t.Fatalf("want --force-with-lease (not plain --force): %v ok=%v", argv, ok)
	}
}

func TestPushForcePlainEmitsForceFlag(t *testing.T) {
	repo, f := pushFakeRepo()
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true, Force: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{"push-force": "force"}})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	argv, ok := pushArgv(f)
	if !ok || !hasArg(argv, "--force") {
		t.Fatalf("want --force: %v ok=%v", argv, ok)
	}
}

func TestPushForceAbortDoesNotPush(t *testing.T) {
	repo, f := pushFakeRepo()
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true, Force: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{"push-force": "abort"}})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Changed {
		t.Fatal("abort must not change anything")
	}
	if _, ok := pushArgv(f); ok {
		t.Fatal("abort must not call git push")
	}
}

// TestPushForceOptionsEscSafe guards that the modal's esc (which resolves to the
// "abort" option) lands on the no-op, never a force. The op offers "abort" by
// name, so esc cannot trigger a history-overwriting push.
func TestPushForceOptionsEscSafe(t *testing.T) {
	repo, f := pushFakeRepo()
	dec := &captureDecider{answers: map[string]string{"push-force": "abort"}}
	if _, err := (Push{Remote: "origin", Branch: "main", Force: true}).Run(
		context.Background(), OpDeps{Repo: repo, Decider: dec}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(dec.seen) == 0 || dec.seen[0].ID != "push-force" {
		t.Fatalf("first decision = %+v, want push-force", dec.seen)
	}
	if !hasArg(dec.seen[0].Options, "abort") {
		t.Fatalf("push-force options must include abort for a safe esc: %v", dec.seen[0].Options)
	}
	// The modal's cursor starts at index 0, so enter acts on options[0]; it must
	// be the SAFER force (lease-protected), never plain --force. Mirrors
	// TestResetModeOptionsLeadWithSafe.
	if dec.seen[0].Options[0] != "force-with-lease" {
		t.Fatalf("push-force options[0] = %q, want force-with-lease (safe enter default)", dec.seen[0].Options[0])
	}
	if _, ok := pushArgv(f); ok {
		t.Fatal("abort path must not push")
	}
}
