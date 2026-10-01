package worktree

import (
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/template"
)

func testCtx() template.Ctx {
	return template.Ctx{
		ParentBranch: "main",
		Repo:         "aaa",
		Seqs:         map[string]int{"issue": 7},
		Now:          func() time.Time { return time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC) },
		Rand:         rand.New(rand.NewPCG(1, 2)),
	}
}

func TestResolveTwoPhase(t *testing.T) {
	tm := Templates{Branch: "issue/<seq:issue>", Path: "../<repo>.worktrees/<branch>"}
	branch, path, err := Resolve(tm, "", nil, testCtx())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if branch != "issue/7" || path != "../aaa.worktrees/issue-7" {
		t.Fatalf("got (%q,%q), want (issue/7, ../aaa.worktrees/issue-7)", branch, path)
	}
}

func TestResolveFixedBranch(t *testing.T) {
	tm := Templates{Branch: "ignored", Path: "wt/<branch>"}
	branch, path, err := Resolve(tm, "hand/edited", nil, testCtx())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if branch != "hand/edited" || path != "wt/hand-edited" {
		t.Fatalf("got (%q,%q), want (hand/edited, wt/hand-edited)", branch, path)
	}
}

func TestResolvePropagatesError(t *testing.T) {
	if _, _, err := Resolve(Templates{Branch: "b-<bogus>", Path: "p/<branch>"}, "", nil, testCtx()); err == nil {
		t.Fatal("expected unknown-token error")
	}
}

func TestLabelsAndSeqNamesUnionInOrder(t *testing.T) {
	tm := Templates{Branch: "<user:user>/<seq:b>", Path: "<user:issue>-<seq:a>-<user:user>"}
	if got := tm.Labels(); !reflect.DeepEqual(got, []string{"user", "issue"}) {
		t.Fatalf("Labels = %v, want [user issue]", got)
	}
	if got := tm.SeqNames(); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("SeqNames = %v, want [b a]", got)
	}
}

func TestRepoName(t *testing.T) {
	if got := RepoName("/work/acme-monorepo"); got != "acme-monorepo" {
		t.Fatalf("RepoName = %q, want acme-monorepo", got)
	}
}

func TestResolvePrefixPeeksMissingSeqsOnly(t *testing.T) {
	gitDir := t.TempDir()
	if _, err := config.BumpSeq(gitDir, "fix"); err != nil { // fix: next = 2
		t.Fatal(err)
	}
	ctx := testCtx() // carries issue = 7 already
	got, names, err := ResolvePrefix("x/<seq:issue>-<seq:fix:3>-<user:n>-<date:yyyy-MM-dd_HH-mm>", map[string]string{"n": "42"}, ctx, gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "x/7-002-42-2026-06-11_00-00" || !reflect.DeepEqual(names, []string{"issue", "fix"}) {
		t.Fatalf("got %q names %v", got, names)
	}
	if _, ok := ctx.Seqs["fix"]; ok {
		t.Fatal("ResolvePrefix must not write into the caller's Seqs map")
	}
}
