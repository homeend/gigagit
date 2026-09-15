package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
)

// metaRefsAt lists the store-format markers a repository carries.
func metaRefsAt(t *testing.T, svc *Service) map[string]int {
	t.Helper()
	got, err := svc.repo.StoreFormats(context.Background())
	if err != nil {
		t.Fatalf("StoreFormats: %v", err)
	}
	return got
}

// TestSnapshotWriterIsGatedOnPreflight is the data-loss guard behind both
// failure paths the preflight spec exists to prevent:
//
//   - the user who chose Skip on a store this build cannot read: the next
//     operation must NOT write a version ref, and must NOT stamp a marker
//     claiming this build's format over the newer data;
//   - an OLDER build opening a store a NEWER gg wrote: reads are refused, and
//     writes must be refused too, so the newer marker survives untouched.
//
// Both reduce to the same repository state: a format-2 marker, which this
// build's versions feature (Min=Max=VersionsFormat=1) resolves as
// Unsatisfiable, so FeatureEnabled("versions") is false. The op runs through
// Execute — the one seam that builds OpDeps — so the test covers the real
// wiring, not snapshotBranchTip in isolation.
func TestSnapshotWriterIsGatedOnPreflight(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	ctx := context.Background()

	empty := gitOutDir(t, dir, "hash-object", "-t", "tree", "/dev/null")
	gitRunDir(t, dir, "", "update-ref", git.MetaRef(StoreVersions, VersionsFormat+1), empty)

	if svc.FeatureEnabled(ctx, FeatureVersions) {
		t.Fatal("a marker one format ahead of this build must disable the versions feature")
	}

	// Commit --amend is a snapshotting op (ops_basic.go snapshots the current
	// branch tip before rewriting it).
	writeFile(t, dir, "f.txt", "changed\n")
	gitRunDir(t, dir, "", "add", "-A")
	if _, err := svc.Execute(ctx, engine.Commit{Message: "amended", Amend: true}, nil, nil); err != nil {
		t.Fatalf("Execute(Commit amend): %v", err)
	}

	infos, err := svc.repo.ForEachRef(ctx, git.VersionRefPrefix)
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	if len(infos) != 0 {
		var refs []string
		for _, i := range infos {
			refs = append(refs, i.Ref)
		}
		t.Errorf("disabled feature still wrote version refs: %s", strings.Join(refs, ", "))
	}

	if got := metaRefsAt(t, svc); got[StoreVersions] != VersionsFormat+1 {
		t.Errorf("StoreFormats = %v, want versions=%d — the newer marker must be untouched",
			got, VersionsFormat+1)
	}
}

// TestSnapshotWriterRunsWhenTheFeatureIsSatisfied is the negative control: the
// same op on a clean repository DOES record a version and stamp this build's
// marker, so the gate above proves a gate and not a broken op.
func TestSnapshotWriterRunsWhenTheFeatureIsSatisfied(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	ctx := context.Background()

	writeFile(t, dir, "f.txt", "changed\n")
	gitRunDir(t, dir, "", "add", "-A")
	if _, err := svc.Execute(ctx, engine.Commit{Message: "amended", Amend: true}, nil, nil); err != nil {
		t.Fatalf("Execute(Commit amend): %v", err)
	}

	infos, err := svc.repo.ForEachRef(ctx, git.VersionRefPrefix)
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	if len(infos) == 0 {
		t.Error("an enabled versions feature recorded no version ref")
	}
	if got := metaRefsAt(t, svc); got[StoreVersions] != VersionsFormat {
		t.Errorf("StoreFormats = %v, want versions=%d", got, VersionsFormat)
	}
}
