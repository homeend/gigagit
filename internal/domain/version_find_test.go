package domain

import (
	"context"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// recordVersion writes one version record the way the engine does (synthetic
// commit + ref) and returns the ref. tip is the snapshotted branch tip.
func recordVersion(t *testing.T, svc *Service, branch, op string, unix int64, tip, base, ours, other string) string {
	t.Helper()
	ctx := context.Background()
	meta := git.VersionMeta{Op: op, Ours: ours, Other: other, Base: base, Source: branch, Target: "onto"}
	syn, err := svc.Repo().WriteVersionSnapshot(ctx, tip, meta, unix)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	ref := git.VersionRef(branch, op, unix)
	if err := svc.Repo().UpdateRef(ctx, ref, syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	return ref
}

// findVersionFixture: three commits c1 < c2 < c3 on main; "feat" froze the
// pair (c1,c2) and "main" froze (c1,c3), BOTH under the id 1700001000-pull —
// what a pull writes for its two branches in one second.
func findVersionFixture(t *testing.T) (svc *Service, c1, c2, c3 string) {
	t.Helper()
	dir := cleanDir(t)
	svc = svcAt(dir)
	c1 = gitOutDir(t, dir, "rev-parse", "main")
	writeFile(t, dir, "g.txt", "2\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "two")
	c2 = gitOutDir(t, dir, "rev-parse", "main")
	writeFile(t, dir, "g.txt", "3\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "three")
	c3 = gitOutDir(t, dir, "rev-parse", "main")
	recordVersion(t, svc, "feat", "pull", 1700001000, c2, c1, c2, c3)
	recordVersion(t, svc, "main", "pull", 1700001000, c3, c1, c3, c2)
	stampVersionsFormat(t, dir)
	return svc, c1, c2, c3
}

func TestFindVersionTieBreaksAnIDByThePair(t *testing.T) {
	t.Parallel()
	svc, c1, c2, c3 := findVersionFixture(t)
	ctx := context.Background()
	branch, v, ok, err := svc.FindVersion(ctx, "1700001000-pull", c1, c3)
	if err != nil || !ok || branch != "main" || v.Ours != c3 {
		t.Fatalf("FindVersion(main's pair) = %q %+v ok=%v err=%v, want main", branch, v, ok, err)
	}
	branch, v, ok, err = svc.FindVersion(ctx, "1700001000-pull", c1, c2)
	if err != nil || !ok || branch != "feat" || v.Ours != c2 {
		t.Fatalf("FindVersion(feat's pair) = %q %+v ok=%v err=%v, want feat", branch, v, ok, err)
	}
}

func TestFindVersionFallsBackToTheIDAloneThenToThePair(t *testing.T) {
	t.Parallel()
	svc, c1, c2, c3 := findVersionFixture(t)
	ctx := context.Background()
	// The id is known but the pair matches neither record: the id still answers.
	if _, v, ok, err := svc.FindVersion(ctx, "1700001000-pull", c2, c3); err != nil || !ok || v.ID() != "1700001000-pull" {
		t.Fatalf("id-only fallback: %+v ok=%v err=%v", v, ok, err)
	}
	// A foreign id (another machine) with a pair this store froze.
	if branch, _, ok, err := svc.FindVersion(ctx, "1600000000-rebase", c1, c3); err != nil || !ok || branch != "main" {
		t.Fatalf("pair-only fallback: branch=%q ok=%v err=%v, want main", branch, ok, err)
	}
	// Nothing matches: a miss, not an error.
	if _, _, ok, err := svc.FindVersion(ctx, "1600000000-rebase", c2, c3); err != nil || ok {
		t.Fatalf("miss: ok=%v err=%v, want false/nil", ok, err)
	}
}

// The store OFF (unstamped legacy format) is a miss: the diff landed and the
// hint has nothing to reveal — never a failure.
func TestFindVersionDisabledStoreIsAMiss(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	c1 := gitOutDir(t, dir, "rev-parse", "main")
	gitRunDir(t, dir, "", "update-ref", git.VersionRef("main", "rebase", 1700002000), c1)
	// No stampVersionsFormat: the versions feature resolves OFF.
	if _, _, ok, err := svc.FindVersion(context.Background(), "1700002000-rebase", c1, c1); err != nil || ok {
		t.Fatalf("disabled store: ok=%v err=%v, want a plain miss", ok, err)
	}
}

// VersionLinkDesc is the ONE description of a version link — the web's row
// desc and DescribeLink's hit arm must not drift apart, and the web must
// not need a store lookup per row to spell it.
func TestVersionLinkDescIsBranchOpAndDate(t *testing.T) {
	t.Parallel()
	v := model.BranchVersion{Op: "rebase", Unix: 1753100000}
	want := "version: main · rebase · " + time.Unix(1753100000, 0).Format("2006-01-02 15:04")
	if got := VersionLinkDesc("main", v); got != want {
		t.Fatalf("VersionLinkDesc = %q, want %q", got, want)
	}
}
