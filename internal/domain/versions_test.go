package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
)

// emptyTreeSHA is git's well-known empty-tree object, always present in any
// repo without a write. stampVersionsFormat points the format-2 marker ref at
// it directly — the fixtures below fabricate version refs with raw
// update-ref (or a raw WriteVersionSnapshot+UpdateRef, bypassing
// snapshotBranchTipNamed entirely), so nothing ever calls the real writer
// that stamps the marker. Without it, BranchVersions/AllVersionBranches see
// an unmarked (format-1) store and the versions feature gates the read.
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

func stampVersionsFormat(t *testing.T, dir string) {
	t.Helper()
	gitRunDir(t, dir, "", "update-ref", git.MetaRef(StoreVersions, VersionsFormat), emptyTreeSHA)
}

// TestBranchVersionsListsNewestFirstAndFiltersBranch fabricates version refs
// directly (raw update-ref) for "main" and "feat/x", then asserts
// BranchVersions returns each branch's own rows, newest first, with Op parsed
// and Subject populated — and that querying the shorter "feat" prefix does
// NOT pick up "feat/x"'s versions (the nested-name over-catch that
// ParseVersionRef's branch check must filter out).
func TestBranchVersionsListsNewestFirstAndFiltersBranch(t *testing.T) {
	t.Parallel()
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
	stampVersionsFormat(t, dir)

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
	t.Parallel()
	dir := cleanDir(t) // main, base commit
	mainSha := gitOutDir(t, dir, "rev-parse", "main")

	const tMain, tGone = int64(1000), int64(5000)
	refMain := git.VersionRef("main", "merge", tMain)
	refGone := git.VersionRef("gone", "restore", tGone)

	gitRunDir(t, dir, "", "update-ref", refMain, mainSha)
	gitRunDir(t, dir, "", "update-ref", refGone, mainSha)
	stampVersionsFormat(t, dir)

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

// TestBranchVersionsSameUnixTieBreaksByRefDescending fabricates two version
// refs for the same branch sharing an identical Unix timestamp (the
// same-second case: two operations snapshotting within the same wall-clock
// second) and asserts BranchVersions falls back to a deterministic Ref
// descending tie-break rather than leaving the order to sort.Slice's
// not-guaranteed-stable comparator on equal keys — checked across two calls
// since map/slice construction order could otherwise vary run to run.
func TestBranchVersionsSameUnixTieBreaksByRefDescending(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t) // main, base commit
	mainSha := gitOutDir(t, dir, "rev-parse", "main")

	const sameTs = int64(4242)
	refMerge := git.VersionRef("main", "merge", sameTs)
	refRestore := git.VersionRef("main", "restore", sameTs)
	// refRestore > refMerge lexicographically (".../4242-restore" >
	// ".../4242-merge") since both share the identical prefix up to the op
	// token, so Ref-descending puts "restore" first.
	if refRestore <= refMerge {
		t.Fatalf("test fixture assumption broken: refRestore=%q must sort after refMerge=%q", refRestore, refMerge)
	}

	gitRunDir(t, dir, "", "update-ref", refMerge, mainSha)
	gitRunDir(t, dir, "", "update-ref", refRestore, mainSha)
	stampVersionsFormat(t, dir)

	svc := svcAt(dir)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		versions, err := svc.BranchVersions(ctx, "main")
		if err != nil {
			t.Fatal(err)
		}
		if len(versions) != 2 {
			t.Fatalf("call %d: versions = %d, want 2: %+v", i, len(versions), versions)
		}
		if versions[0].Unix != sameTs || versions[1].Unix != sameTs {
			t.Fatalf("call %d: expected both rows at Unix=%d: %+v", i, sameTs, versions)
		}
		if versions[0].Op != "restore" || versions[1].Op != "merge" {
			t.Fatalf("call %d: tie-break order = %+v, want restore then merge (Ref descending)", i, versions)
		}
	}
}

// TestAllVersionBranchesSameUnixTieBreaksByBranchAscending fabricates two
// branches whose LatestUnix values are identical (the same-second case) and
// asserts AllVersionBranches falls back to a deterministic Branch ascending
// tie-break — checked across two calls for stability, since the rows are
// built by ranging a map before sorting.
func TestAllVersionBranchesSameUnixTieBreaksByBranchAscending(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t) // main, base commit
	mainSha := gitOutDir(t, dir, "rev-parse", "main")

	const sameTs = int64(7777)
	refAlpha := git.VersionRef("alpha", "merge", sameTs)
	refZulu := git.VersionRef("zulu", "merge", sameTs)

	gitRunDir(t, dir, "", "update-ref", refAlpha, mainSha)
	gitRunDir(t, dir, "", "update-ref", refZulu, mainSha)
	stampVersionsFormat(t, dir)

	svc := svcAt(dir)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		rows, err := svc.AllVersionBranches(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("call %d: rows = %d, want 2: %+v", i, len(rows), rows)
		}
		if rows[0].LatestUnix != sameTs || rows[1].LatestUnix != sameTs {
			t.Fatalf("call %d: expected both rows at LatestUnix=%d: %+v", i, sameTs, rows)
		}
		if rows[0].Branch != "alpha" || rows[1].Branch != "zulu" {
			t.Fatalf("call %d: tie-break order = %+v, want alpha then zulu (Branch ascending)", i, rows)
		}
	}
}

// TestExecuteInjectsVersionsPolicy pins the wiring Task 6 turns on: Execute's
// OpDeps literal now carries the Service's versions policy, so an op that
// snapshots on amend (see engine.Commit) actually records a version ref when
// run through Execute with the default policy — and records nothing once the
// policy is disabled via SetVersionsPolicy.
func TestExecuteInjectsVersionsPolicy(t *testing.T) {
	t.Parallel()
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

// TestBranchVersionsUnwrapsTheSnapshotCommit writes a real version snapshot
// (WriteVersionSnapshot + UpdateRef, the production path) and asserts
// BranchVersions unwraps the synthetic commit: Hash is the snapshotted tip
// (not the synthetic commit), the recorded endpoints come back on the
// model.BranchVersion, and Subject is the TIP's own subject — not the
// synthetic commit's "gg version snapshot (rebase)" subject, which is what
// %(subject) alone would give every row.
func TestBranchVersionsUnwrapsTheSnapshotCommit(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	ctx := context.Background()

	tip, err := svc.Repo().RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	meta := git.VersionMeta{Op: "rebase", Ours: tip, Other: tip, Base: tip, Source: "feat/x", Target: "main"}
	syn, err := svc.Repo().WriteVersionSnapshot(ctx, tip, meta, 1700000000)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	if err := svc.Repo().UpdateRef(ctx, git.VersionRef("main", "rebase", 1700000000), syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	stampVersionsFormat(t, dir)

	vs, err := svc.BranchVersions(ctx, "main")
	if err != nil {
		t.Fatalf("BranchVersions: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("got %d versions, want 1", len(vs))
	}
	v := vs[0]
	// Hash must be the snapshotted tip, NOT the synthetic commit — restore and
	// every UI that shows the recorded commit depend on this.
	if v.Hash != tip {
		t.Errorf("Hash = %s, want the snapshotted tip %s (synthetic was %s)", v.Hash, tip, syn)
	}
	if v.Ours != tip || v.Base != tip || v.Source != "feat/x" || v.Target != "main" {
		t.Errorf("endpoints = %+v, want the recorded meta", v)
	}
	if v.Op != "rebase" {
		t.Errorf("Op = %q, want rebase", v.Op)
	}
	// Subject must be the snapshotted TIP's own subject ("base", from
	// cleanDir), not the synthetic commit's "gg version snapshot (rebase)" —
	// without subjectsOf wired in, every row would render identically.
	if v.Subject != "base" {
		t.Errorf("Subject = %q, want the tip's own subject %q (not the synthetic wrapper's)", v.Subject, "base")
	}
}
