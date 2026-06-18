package engine

import (
	"context"
	"testing"
)

// configRemote makes refs/remotes/origin/* genuine remote-tracking branches so
// `git branch --track origin/*` works, as in a real fetched repo.
func configRemote(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "config", "remote.origin.url", "file://"+dir)
	gitIn(t, dir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
}

func TestSmartCheckoutAbsentLocalStay(t *testing.T) {
	dir, repo := newRepo(t)
	configRemote(t, dir)
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	res, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutStay}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if ok, _ := repo.LocalBranchExists(context.Background(), "foo"); !ok {
		t.Fatal("local foo was not created")
	}
	if cur, _ := repo.CurrentBranch(context.Background()); cur == "foo" {
		t.Fatal("CheckoutStay must not switch")
	}
}

func TestSmartCheckoutAbsentLocalSwitch(t *testing.T) {
	dir, repo := newRepo(t)
	configRemote(t, dir)
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	_, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutSwitch}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("checkout+switch: %v", err)
	}
	if cur, _ := repo.CurrentBranch(context.Background()); cur != "foo" {
		t.Fatalf("current = %q, want foo", cur)
	}
}

func TestSmartCheckoutExistingBehindFastForwards(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD") // ahead
	gitIn(t, dir, "branch", "foo", "HEAD~1")                       // behind
	_, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutStay}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("ff checkout: %v", err)
	}
	if ok, _ := repo.IsAncestor(context.Background(), "refs/remotes/origin/foo", "foo"); !ok {
		t.Fatal("foo was not fast-forwarded to origin/foo")
	}
}

func TestSmartCheckoutDivergedRefuses(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "foo")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "main-only")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD") // origin/foo on main's line
	gitIn(t, dir, "checkout", "foo")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "foo-only") // foo diverges
	gitIn(t, dir, "checkout", "main")
	_, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutStay}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil {
		t.Fatal("diverged local foo must refuse")
	}
}

func TestSmartCheckoutCurrentBranchRefuses(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "checkout", "-b", "foo")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	_, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutStay}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil {
		t.Fatal("checkout of the current branch must refuse")
	}
}
