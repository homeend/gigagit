package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
)

// TestBranchVersionsListsNewestFirstAndFiltersBranch fabricates version refs
// directly (raw update-ref) for "main" and "feat/x", then asserts
// BranchVersions returns each branch's own rows, newest first, with Op parsed
// and Subject populated — and that querying the shorter "feat" prefix does
// NOT pick up "feat/x"'s versions (the nested-name over-catch that
// ParseVersionRef's branch check must filter out).
func TestBranchVersionsListsNewestFirstAndFiltersBranch(t *testing.T) {
	dir := cleanDir(t) // main, base commit, f.txt = "hi\n"
	gitRunDir(t, dir, "", "checkout", "-q", "-b", "feat/x")
	writeFile(t, dir, "g.txt", "hi\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "feat commit")
	gitRunDir(t, dir, "", "checkout", "-q", "main")

	mainSha := gitOutDir(t, dir, "rev-parse", "main")
	featSha := gitOutDir(t, dir, "rev-parse", "feat/x")

	const t1, t2, t3 = int64(1000), int64(2000), int64(1500)
	refMain1 := git.VersionRef("main", "merge", t1)
	refMain2 := git.VersionRef("main", "rebase", t2)
	refFeat := git.VersionRef("feat/x", "restore", t3)

	gitRunDir(t, dir, "", "update-ref", refMain1, mainSha)
	gitRunDir(t, dir, "", "update-ref", refMain2, mainSha)
	gitRunDir(t, dir, "", "update-ref", refFeat, featSha)

	svc := svcAt(dir)
	ctx := context.Background()

	mainVersions, err := svc.BranchVersions(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(mainVersions) != 2 {
		t.Fatalf("main versions = %d, want 2: %+v", len(mainVersions), mainVersions)
	}
	if mainVersions[0].Unix != t2 || mainVersions[1].Unix != t1 {
		t.Fatalf("main versions not newest-first: %+v", mainVersions)
	}
	if mainVersions[0].Op != "rebase" || mainVersions[1].Op != "merge" {
		t.Fatalf("Op not parsed correctly: %+v", mainVersions)
	}
	for _, v := range mainVersions {
		if v.Subject == "" {
			t.Errorf("version %+v has empty Subject", v)
		}
		if v.Hash != mainSha {
			t.Errorf("version %+v Hash = %q, want %q", v, v.Hash, mainSha)
		}
	}

	featVersions, err := svc.BranchVersions(ctx, "feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if len(featVersions) != 1 {
		t.Fatalf("feat/x versions = %d, want 1: %+v", len(featVersions), featVersions)
	}
	if featVersions[0].Op != "restore" || featVersions[0].Unix != t3 {
		t.Fatalf("feat/x version wrong: %+v", featVersions[0])
	}

	// Nested-name safety: versions of "feat/x" must NOT appear for "feat".
	nestedVersions, err := svc.BranchVersions(ctx, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if len(nestedVersions) != 0 {
		t.Fatalf("feat versions = %+v, want none (nested-name safety)", nestedVersions)
	}
}

// TestAllVersionBranchesMarksDeleted fabricates version refs for "main" (a
// real branch) and "gone" (no matching refs/heads entry), then asserts
// AllVersionBranches reports both, correctly marking "gone" as Deleted and
// sorting by LatestUnix descending.
func TestAllVersionBranchesMarksDeleted(t *testing.T) {
	dir := cleanDir(t) // main, base commit
	mainSha := gitOutDir(t, dir, "rev-parse", "main")

	const tMain, tGone = int64(1000), int64(5000)
	refMain := git.VersionRef("main", "merge", tMain)
	refGone := git.VersionRef("gone", "restore", tGone)

	gitRunDir(t, dir, "", "update-ref", refMain, mainSha)
	gitRunDir(t, dir, "", "update-ref", refGone, mainSha)

	svc := svcAt(dir)
	rows, err := svc.AllVersionBranches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(rows), rows)
	}
	if rows[0].Branch != "gone" || !rows[0].Deleted || rows[0].Count != 1 {
		t.Fatalf("rows[0] = %+v, want {Branch:gone Deleted:true Count:1}", rows[0])
	}
	if rows[1].Branch != "main" || rows[1].Deleted {
		t.Fatalf("rows[1] = %+v, want {Branch:main Deleted:false}", rows[1])
	}
	if rows[0].LatestUnix != tGone || rows[1].LatestUnix != tMain {
		t.Fatalf("LatestUnix order wrong: %+v", rows)
	}
}

// TestExecuteInjectsVersionsPolicy pins the wiring Task 6 turns on: Execute's
// OpDeps literal now carries the Service's versions policy, so an op that
// snapshots on amend (see engine.Commit) actually records a version ref when
// run through Execute with the default policy — and records nothing once the
// policy is disabled via SetVersionsPolicy.
func TestExecuteInjectsVersionsPolicy(t *testing.T) {
	dir := cleanDir(t) // main, base commit, f.txt = "hi\n"
	svc := svcAt(dir)
	ctx := context.Background()

	writeFile(t, dir, "f.txt", "dirty\n")
	if _, err := svc.Execute(ctx, engine.Stage{All: true}, nil, nil); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, err := svc.Execute(ctx, engine.Commit{Message: "amend one", Amend: true}, nil, nil); err != nil {
		t.Fatalf("amend commit: %v", err)
	}

	versions, err := svc.BranchVersions(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions after first amend = %d, want 1: %+v", len(versions), versions)
	}
	if versions[0].Op != "amend" {
		t.Fatalf("Op = %q, want amend", versions[0].Op)
	}

	if svc.SetVersionsPolicy(engine.VersionsPolicy{Enabled: false}) != svc {
		t.Fatal("SetVersionsPolicy must return the Service for chaining")
	}

	writeFile(t, dir, "f.txt", "dirty2\n")
	if _, err := svc.Execute(ctx, engine.Stage{All: true}, nil, nil); err != nil {
		t.Fatalf("stage 2: %v", err)
	}
	if _, err := svc.Execute(ctx, engine.Commit{Message: "amend two", Amend: true}, nil, nil); err != nil {
		t.Fatalf("amend commit 2: %v", err)
	}

	versionsAfter, err := svc.BranchVersions(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(versionsAfter) != 1 {
		t.Fatalf("versions after disabled-policy amend = %d, want still 1 (no new ref): %+v", len(versionsAfter), versionsAfter)
	}
}
