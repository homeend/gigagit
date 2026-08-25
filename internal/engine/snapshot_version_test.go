package engine

import (
	"context"
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

	snapshotBranchTip(ctx, deps, "main", "rebase")
	if got := versionRefs(t, repo); len(got) != 0 {
		t.Fatalf("disabled policy wrote %v", got)
	}

	deps.Versions = VersionsPolicy{Enabled: true, MaxAgeDays: 90}
	snapshotBranchTip(ctx, deps, "", "rebase") // detached HEAD: no branch
	if got := versionRefs(t, repo); len(got) != 0 {
		t.Fatalf("empty branch wrote %v", got)
	}

	snapshotBranchTip(ctx, deps, "main", "rebase")
	got := versionRefs(t, repo)
	if len(got) != 1 || !strings.Contains(got[0], "/main/") || !strings.HasSuffix(got[0], "-rebase") {
		t.Fatalf("refs = %v", got)
	}
	head, _ := repo.RevParse(ctx, "HEAD")
	infos, _ := repo.ForEachRef(ctx, "refs/gg/versions")
	if infos[0].Hash != head {
		t.Fatalf("snapshot points at %s, want %s", infos[0].Hash, head)
	}

	// Second snapshot in the same second must not collide (ts bumps).
	snapshotBranchTip(ctx, deps, "main", "rebase")
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
	snapshotBranchTip(ctx, OpDeps{Repo: repo, Versions: VersionsPolicy{Enabled: true, MaxAgeDays: -1}}, "main", "rebase")
	if got := versionRefs(t, repo); len(got) != 2 {
		t.Fatalf("forever policy pruned: %v", got)
	}

	// 90 days: the 120-day-old ref is pruned on the next write.
	snapshotBranchTip(ctx, OpDeps{Repo: repo, Versions: VersionsPolicy{Enabled: true, MaxAgeDays: 90}}, "main", "rebase")
	for _, ref := range versionRefs(t, repo) {
		if ref == oldRef {
			t.Fatalf("expired ref survived: %v", versionRefs(t, repo))
		}
	}
}
