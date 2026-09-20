package domain

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// baseFixture: main (root + c1), feat/x one commit ahead touching feat.txt.
// No remote, no upstream: each test adds the one thing it is about.
func baseFixture(t *testing.T) (dir string, svc *Service, abs string) {
	t.Helper()
	dir, svc = newRealRepo(t)
	gitIn(t, dir, "branch", "-M", "main")
	writeIn(t, dir, "base.txt", "base\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "c1")
	gitIn(t, dir, "checkout", "-qb", "feat/x")
	writeIn(t, dir, "feat.txt", "feat\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "feat work")
	gitIn(t, dir, "checkout", "-q", "main")
	top, err := svc.TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return dir, svc, linkAbs(top)
}

func fakeOriginHead(t *testing.T, dir, branch string) {
	t.Helper()
	gitIn(t, dir, "update-ref", "refs/remotes/origin/"+branch, "main")
	gitIn(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/"+branch)
}

func gitText(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func suggest(t *testing.T, svc *Service, text string) BaseSuggestion {
	t.Helper()
	l, err := model.ParseLink(text)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.SuggestBase(context.Background(), l, ResolveOpts{})
	if err != nil {
		t.Fatalf("SuggestBase(%s): %v", text, err)
	}
	return got
}

func TestSuggestBasePrefersTheUpstream(t *testing.T) {
	t.Parallel()
	dir, svc, abs := baseFixture(t)
	fakeOriginHead(t, dir, "trunk")
	gitIn(t, dir, "config", "branch.feat/x.remote", ".")
	gitIn(t, dir, "config", "branch.feat/x.merge", "refs/heads/main")
	got := suggest(t, svc, "gg://"+abs+"@ref:feat/x")
	if got.Kind != model.LinkBoundRef || got.Base != "main" || got.Why != "upstream" {
		t.Errorf("got %+v, want the upstream (main) over the trunk (origin/trunk)", got)
	}
}

func TestSuggestBaseFallsBackToOriginHEAD(t *testing.T) {
	t.Parallel()
	dir, svc, abs := baseFixture(t)
	fakeOriginHead(t, dir, "trunk")
	got := suggest(t, svc, "gg://"+abs+"@ref:feat/x")
	if got.Base != "origin/trunk" || got.Why != "trunk" {
		t.Errorf("got %+v, want origin's default branch over a local main", got)
	}
}

func TestSuggestBaseFallsBackToLocalMainThenMaster(t *testing.T) {
	t.Parallel()
	dir, svc, abs := baseFixture(t)
	if got := suggest(t, svc, "gg://"+abs+"@ref:feat/x"); got.Base != "main" || got.Why != "trunk" {
		t.Errorf("got %+v, want local main", got)
	}
	gitIn(t, dir, "branch", "-m", "main", "master")
	svc2 := Open(dir) // a fresh service: the first cached "refs/heads/main resolves"
	if got := suggest(t, svc2, "gg://"+abs+"@ref:feat/x"); got.Base != "master" || got.Why != "trunk" {
		t.Errorf("got %+v, want local master once there is no main", got)
	}
}

// A branch is not bounded by itself: the walk skips it and goes on.
func TestSuggestBaseSkipsTheRefItself(t *testing.T) {
	t.Parallel()
	dir, svc, abs := baseFixture(t)
	got := suggest(t, svc, "gg://"+abs+"@ref:main")
	if got.Kind != model.LinkBoundRef || got.Base != "" || got.Why != "" {
		t.Errorf("got %+v, want the row offered EMPTY — main is the only candidate and it is the ref", got)
	}
	gitIn(t, dir, "branch", "master", "main")
	if got := suggest(t, Open(dir), "gg://"+abs+"@ref:main"); got.Base != "master" {
		t.Errorf("got %+v, want the walk to continue past main to master", got)
	}
}

func TestSuggestBaseForACommitIsItsFirstParentAndItsOwnFullSha(t *testing.T) {
	t.Parallel()
	dir, svc, abs := baseFixture(t)
	head := headHashFull(t, dir)
	parent := gitText(t, dir, "rev-parse", "HEAD^")
	got := suggest(t, svc, "gg://"+abs+"@"+head[:7])
	if got.Kind != model.LinkBoundCommit || got.Self != head || got.Base != parent || got.Why != "parent" {
		t.Errorf("got %+v, want Self=%s Base=%s (parent)", got, head, parent)
	}
	root := gitText(t, dir, "rev-list", "--max-parents=0", "HEAD")
	got = suggest(t, svc, "gg://"+abs+"@"+root)
	if got.Kind != model.LinkBoundCommit || got.Self != root || got.Base != "" || got.Why != "" {
		t.Errorf("root: got %+v, want the row offered empty", got)
	}
}

// The LOCAL-form row. The pure BoundKind cannot tell gg:///abs/f@ref:x from a
// whole tree (the file is still inside Repo.Abs); the located answer can, and
// a file link is already bounded — no base row.
func TestSuggestBaseSeesThroughALocalFormFileLink(t *testing.T) {
	t.Parallel()
	_, svc, abs := baseFixture(t)
	text := "gg://" + abs + "/base.txt@ref:feat/x"
	l, err := model.ParseLink(text)
	if err != nil {
		t.Fatal(err)
	}
	if l.BoundKind() != model.LinkBoundRef {
		t.Fatalf("fixture: the pure kind is %v, want Ref — the two answers must differ", l.BoundKind())
	}
	if got := suggest(t, svc, text); got.Kind != model.LinkBoundNone || got.Base != "" {
		t.Errorf("got %+v, want no base row for a file link", got)
	}
	if got := suggest(t, svc, "gg://"+abs+"@ref:feat/x"); got.Kind != model.LinkBoundRef {
		t.Errorf("whole-tree twin: got %+v, want a base row", got)
	}
}

func TestSuggestBaseHasNothingForALinkItCannotLocate(t *testing.T) {
	t.Parallel()
	_, svc, _ := baseFixture(t)
	if got := suggest(t, svc, "gg://nowhere-on-this-machine@ref:main"); got != (BaseSuggestion{}) {
		t.Errorf("got %+v, want the zero suggestion", got)
	}
}

// End to end: the rewrite's output goes through the door, with a
// REMOTE-TRACKING target — the shape origin/HEAD suggests.
func TestABoundedRefLinkComparesThroughTheDoor(t *testing.T) {
	t.Parallel()
	dir, svc, abs := baseFixture(t)
	fakeOriginHead(t, dir, "trunk")
	text := "gg://" + abs + "@ref:feat/x"
	l, _ := model.ParseLink(text)
	sug := suggest(t, svc, text)
	bounded, ok := l.WithBase(sug.Base, sug.Self)
	if !ok {
		t.Fatalf("WithBase(%q) refused", sug.Base)
	}
	c, err := svc.CompareLinks(context.Background(), "gg://"+abs+"@ref:main", bounded.String(), ResolveOpts{})
	if err != nil {
		t.Fatalf("CompareLinks(%s): %v", bounded.String(), err)
	}
	wantRows(t, c.Files, "A feat.txt")
}
