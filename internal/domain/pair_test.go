package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"os/exec"
	"strings"

	"github.com/homeend/gigagit/internal/gittest"
)

func revOf(t *testing.T, dir, rev string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", rev).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestPairAddFreezesNamesToFullShas(t *testing.T) {
	t.Parallel()
	dir, svc := previewRepo(t)
	ctx := context.Background()
	wantA, wantB := revOf(t, dir, "main"), revOf(t, dir, "feat/x")
	p, err := svc.PairAdd(ctx, "main", "feat/x", "")
	if err != nil || p.A != wantA || p.B != wantB {
		t.Fatalf("add = %+v, %v; want A=%s B=%s", p, err, wantA, wantB)
	}
	if p.Label != wantA[:7]+".."+wantB[:7] {
		t.Fatalf("default label = %q", p.Label)
	}
	// Advance feat/x: a frozen pair must not follow it.
	gittest.Run(t, dir, "checkout", "-q", "feat/x")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c\n"), 0o644)
	gittest.Run(t, dir, "add", "c.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "add c")
	got, err := svc.PairGet(ctx, p.ID)
	if err != nil || got.B != wantB {
		t.Fatalf("after the branch moved: %+v, %v; want B still %s", got, err, wantB)
	}
}

func TestPairAddRefusals(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	if _, err := svc.PairAdd(ctx, "main", "main", ""); err == nil {
		t.Fatal("same commit accepted")
	}
	// Two SPELLINGS of one commit are the same commit.
	if _, err := svc.PairAdd(ctx, "main", "main^{commit}", ""); err == nil {
		t.Fatal("same commit under two spellings accepted")
	}
	if _, err := svc.PairAdd(ctx, "main", "no-such-rev", ""); err == nil {
		t.Fatal("unknown rev accepted")
	}
}

func TestPairAddDuplicateReturnsStoredRow(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	first, err := svc.PairAdd(ctx, "main", "feat/x", "mine")
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.PairAdd(ctx, "main", "feat/x", "other label")
	if !errors.Is(err, ErrPairExists) || again.ID != first.ID || again.Label != "mine" {
		t.Fatalf("duplicate = %+v, %v", again, err)
	}
}

// The two recognisers must DISAGREE on one fixture: three legitimate rows,
// each surface sees exactly its own.
func TestPairAndPreviewRecognisersDisagree(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	pv, err := svc.PreviewAdd(ctx, "feat/x", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	pr, err := svc.PairAdd(ctx, "main", "feat/x", "")
	if err != nil {
		t.Fatal(err)
	}
	all0, _ := svc.SavedCompareList(ctx)
	if _, err := svc.SavedCompareAdd(ctx, all0[0].Left, all0[1].Left, "cmp"); err != nil {
		t.Fatal(err)
	}
	previews, _ := svc.PreviewList(ctx)
	pairs, _ := svc.PairList(ctx)
	all, _ := svc.SavedCompareList(ctx)
	if len(all) != 3 || len(previews) != 1 || previews[0].ID != pv.ID || len(pairs) != 1 || pairs[0].ID != pr.ID {
		t.Fatalf("all=%d previews=%v pairs=%v", len(all), previews, pairs)
	}
}

func TestPairRecogniserRejectsShapesItDidNotProduce(t *testing.T) {
	t.Parallel()
	dir, svc := previewRepo(t)
	ctx := context.Background()
	a, b := revOf(t, dir, "main"), revOf(t, dir, "feat/x")
	base := "gg://gigagit"
	for _, left := range []string{
		base + "@main..feat/x",          // names, not shas
		base + "@" + a[:8] + ".." + b,   // a short sha
		base + "/a.txt@" + a + ".." + b, // path-narrowed
	} {
		if _, err := svc.SavedCompareAdd(ctx, left, "", "x"); err != nil {
			t.Fatalf("fixture %s: %v", left, err)
		}
	}
	if pairs, _ := svc.PairList(ctx); len(pairs) != 0 {
		t.Fatalf("recognised foreign shapes: %+v", pairs)
	}
}

func TestPairRenameRemoveOnlyTouchPairs(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	pv, _ := svc.PreviewAdd(ctx, "feat/x", "main", "")
	pr, _ := svc.PairAdd(ctx, "main", "feat/x", "")
	if err := svc.PairRename(ctx, pv.ID, "x"); !errors.Is(err, ErrPairNotFound) {
		t.Fatalf("renamed a PREVIEW through the pair surface: %v", err)
	}
	if err := svc.PairRemove(ctx, pv.ID); !errors.Is(err, ErrPairNotFound) {
		t.Fatalf("removed a PREVIEW through the pair surface: %v", err)
	}
	if err := svc.PairRename(ctx, pr.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.PairGet(ctx, "renamed"); got.ID != pr.ID {
		t.Fatal("get by new label failed")
	}
	if err := svc.PairRemove(ctx, pr.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PairGet(ctx, pr.ID); !errors.Is(err, ErrPairNotFound) {
		t.Fatalf("get after remove = %v", err)
	}
}
