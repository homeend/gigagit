package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
)

func versionRefs(t *testing.T, r *git.Repo) []string {
	t.Helper()
	infos, err := r.ForEachRef(context.Background(), "refs/gg/versions")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, i := range infos {
		out = append(out, i.Ref)
	}
	return out
}

func TestSnapshotBranchTipRecordsAndSkips(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ctx := context.Background()
	deps := OpDeps{Repo: repo} // zero policy: disabled

	snapshotBranchTip(ctx, deps, "main", "rebase", "", "")
	if got := versionRefs(t, repo); len(got) != 0 {
		t.Fatalf("disabled policy wrote %v", got)
	}

	deps.Versions = VersionsPolicy{Enabled: true, MaxAgeDays: 90, Format: 1}
	snapshotBranchTip(ctx, deps, "", "rebase", "", "") // detached HEAD: no branch
	if got := versionRefs(t, repo); len(got) != 0 {
		t.Fatalf("empty branch wrote %v", got)
	}

	snapshotBranchTip(ctx, deps, "main", "rebase", "", "")
	got := versionRefs(t, repo)
	if len(got) != 1 || !strings.Contains(got[0], "/main/") || !strings.HasSuffix(got[0], "-rebase") {
		t.Fatalf("refs = %v", got)
	}
	// The ref points at a synthetic wrapper commit, not head directly —
	// VersionRefs unwraps to the snapshotted tip via the first parent.
	head, _ := repo.RevParse(ctx, "HEAD")
	bvs, err := repo.VersionRefs(ctx, "refs/gg/versions")
	if err != nil {
		t.Fatal(err)
	}
	if bvs[0].Hash != head {
		t.Fatalf("snapshot unwraps to %s, want %s", bvs[0].Hash, head)
	}

	// Second snapshot in the same second must not collide (ts bumps).
	snapshotBranchTip(ctx, deps, "main", "rebase", "", "")
	if got := versionRefs(t, repo); len(got) != 2 {
		t.Fatalf("collision handling: %v", got)
	}
}

func TestSnapshotBranchTipPrunes(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ctx := context.Background()
	head, _ := repo.RevParse(ctx, "HEAD")
	old := time.Now().AddDate(0, 0, -120).Unix()
	oldRef := git.VersionRef("main", "merge", old)
	if err := repo.UpdateRef(ctx, oldRef, head); err != nil {
		t.Fatal(err)
	}

	// MaxAgeDays -1 (forever): the old ref survives a new snapshot.
	snapshotBranchTip(ctx, OpDeps{Repo: repo, Versions: VersionsPolicy{Enabled: true, MaxAgeDays: -1, Format: 1}}, "main", "rebase", "", "")
	if got := versionRefs(t, repo); len(got) != 2 {
		t.Fatalf("forever policy pruned: %v", got)
	}

	// 90 days: the 120-day-old ref is pruned on the next write.
	snapshotBranchTip(ctx, OpDeps{Repo: repo, Versions: VersionsPolicy{Enabled: true, MaxAgeDays: 90, Format: 1}}, "main", "rebase", "", "")
	for _, ref := range versionRefs(t, repo) {
		if ref == oldRef {
			t.Fatalf("expired ref survived: %v", versionRefs(t, repo))
		}
	}
}

func TestSnapshotBranchTipStampsTheStoreFormat(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ctx := context.Background()
	deps := OpDeps{Repo: repo, Versions: VersionsPolicy{Enabled: true, MaxAgeDays: 90, Format: 1}}

	snapshotBranchTip(ctx, deps, "main", "rebase", "", "")

	formats, err := deps.Repo.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats: %v", err)
	}
	if formats["versions"] != 1 {
		t.Errorf("StoreFormats = %v, want versions=1 — the writer must stamp the marker", formats)
	}
}

// TestSnapshotBranchTipRecordsEndpoints is Task 5's Step 1 test: a two-branch
// op (ours/other both set) must record Ours/Other verbatim and Base as their
// independently-computed merge-base, on top of the usual pre-op Hash.
func TestSnapshotBranchTipRecordsEndpoints(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()

	// Diverge main and feat from the same base so merge-base is non-trivial
	// (not just one of the two tips).
	if err := repo.CreateBranch(ctx, "feat", ""); err != nil {
		t.Fatal(err)
	}
	gitE(t, dir, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(dir, "m.txt"), []byte("m\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-qm", "main work")
	gitE(t, dir, "checkout", "-q", "feat")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("f\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-qm", "feat work")
	gitE(t, dir, "checkout", "-q", "main")

	oursSha, err := repo.RevParse(ctx, "feat")
	if err != nil {
		t.Fatal(err)
	}
	otherSha, err := repo.RevParse(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	wantBase, err := repo.MergeBase(ctx, oursSha, otherSha)
	if err != nil {
		t.Fatal(err)
	}

	preOpTip := otherSha
	deps := enabledDeps(repo)
	snapshotBranchTip(ctx, deps, "main", "rebase", oursSha, otherSha)

	bvs, err := repo.VersionRefs(ctx, git.VersionRefPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(bvs) != 1 {
		t.Fatalf("version refs = %d, want 1: %+v", len(bvs), bvs)
	}
	bv := bvs[0]
	if bv.Hash != preOpTip {
		t.Errorf("Hash = %s, want the pre-op tip %s", bv.Hash, preOpTip)
	}
	if bv.Ours != oursSha {
		t.Errorf("Ours = %s, want %s", bv.Ours, oursSha)
	}
	if bv.Other != otherSha {
		t.Errorf("Other = %s, want %s", bv.Other, otherSha)
	}
	if bv.Base != wantBase {
		t.Errorf("Base = %s, want merge-base %s", bv.Base, wantBase)
	}
	// The short form's Source/Target default: Source is the snapshotted
	// branch, Target is other exactly as given (real call sites pass a name
	// here — smart_rebase.go's op.Onto — this test passes otherSha directly
	// to keep the fixture self-contained).
	if bv.Source != "main" {
		t.Errorf("Source = %q, want main", bv.Source)
	}
	if bv.Target != otherSha {
		t.Errorf("Target = %q, want %s", bv.Target, otherSha)
	}
}

// TestSnapshotBranchTipOneBranchOpRecordsNoEndpoints is Task 5's Step 1 test:
// a one-branch op (ours/other both "") must record no preview endpoints while
// Hash and Op still come back right.
func TestSnapshotBranchTipOneBranchOpRecordsNoEndpoints(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ctx := context.Background()
	head, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	snapshotBranchTip(ctx, enabledDeps(repo), "main", "reset", "", "")

	bvs, err := repo.VersionRefs(ctx, git.VersionRefPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(bvs) != 1 {
		t.Fatalf("version refs = %d, want 1: %+v", len(bvs), bvs)
	}
	bv := bvs[0]
	if bv.Hash != head {
		t.Errorf("Hash = %s, want %s", bv.Hash, head)
	}
	if bv.Op != "reset" {
		t.Errorf("Op = %q, want reset", bv.Op)
	}
	if bv.Ours != "" || bv.Other != "" || bv.Base != "" {
		t.Errorf("endpoints = %+v, want all empty for a one-branch op", bv)
	}
}
