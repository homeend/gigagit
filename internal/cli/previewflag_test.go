package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// The three-dot form needs no saved record, and the resolved DiffSpec is the
// PREVIEW's patch, built by the one constructor (set.DiffSpec()) — so hunk
// numbers under --preview match `gg diff --preview --hunks` and are never the
// tip commit's own parent→tip numbering.
//
// previewRepo (preview_test.go) sets XDG_STATE_HOME/HOME via t.Setenv, so
// this test — like every other caller of previewRepo — stays serial.
func TestResolvePreviewTargetThreeDotForm(t *testing.T) {
	dir := previewRepo(t)
	svc := domain.Open(dir)
	tgt, err := resolvePreviewTarget(context.Background(), svc, "main...feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Source != "feat/x" || tgt.Target != "main" {
		t.Fatalf("got %s/%s", tgt.Source, tgt.Target)
	}
	if !tgt.Set.OK() || tgt.Set.Tip != runGit(t, dir, "rev-parse", "feat/x") {
		t.Fatalf("the set's tip must be feat/x's tip, got %+v", tgt.Set)
	}
	if tgt.Spec.Rev != tgt.Set.DiffSpec().Rev {
		t.Fatalf("the CLI must not build its own range: %q vs %q", tgt.Spec.Rev, tgt.Set.DiffSpec().Rev)
	}
	// The preview's patch runs merge-base → tip, NOT parent(tip) → tip. main
	// keeps moving after the branch point in previewRepo's shape, so compare
	// against the actual merge-base rather than main's current tip.
	base := runGit(t, dir, "merge-base", "main", "feat/x")
	if !strings.HasPrefix(tgt.Spec.Rev, base) {
		t.Fatalf("the range must start at the merge base %q, got %q", base, tgt.Spec.Rev)
	}
}

func TestDiffPreviewRefusesASecondTarget(t *testing.T) {
	dir := previewRepo(t)
	svc := domain.Open(dir)
	var out, errb strings.Builder
	code := cmdDiff(svc, dir, []string{"--preview", "main...feat/x", "--cached"}, &out, &errb)
	if code != 2 {
		t.Fatalf("want exit 2, got %d", code)
	}
	if !strings.Contains(errb.String(), "one target only") {
		t.Fatalf("want the one-target message, got %q", errb.String())
	}
}

func TestDiffPreviewHunksNumberThePreviewDiff(t *testing.T) {
	dir := previewRepo(t)
	svc := domain.Open(dir)
	var out, errb strings.Builder
	if code := cmdDiff(svc, dir, []string{"--preview", "main...feat/x", "--hunks"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "a.txt") || !strings.Contains(out.String(), "  1 @@") {
		t.Fatalf("want numbered hunks over the preview diff, got %q", out.String())
	}
}
