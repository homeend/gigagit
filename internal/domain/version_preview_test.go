package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// TestVersionPreviewReturnsEndpoints writes a real version snapshot
// (WriteVersionSnapshot + UpdateRef, the production path) with three DISTINCT
// recorded endpoints (base, ours, other) and asserts VersionPreview maps them
// to the right sides of the compare pipeline: Left = the recorded Base,
// Right = the recorded Ours — not Other, not the snapshot tip, and not a
// swap of the two.
func TestVersionPreviewReturnsEndpoints(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t) // main, base commit, f.txt = "hi\n"
	svc := svcAt(dir)
	ctx := context.Background()

	baseSha, err := svc.Repo().RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatalf("RevParse base: %v", err)
	}
	gitRunDir(t, dir, "", "checkout", "-q", "-b", "feat/x")
	writeFile(t, dir, "g.txt", "hi\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "feat commit")
	oursSha := gitOutDir(t, dir, "rev-parse", "feat/x")

	gitRunDir(t, dir, "", "checkout", "-q", "main")
	writeFile(t, dir, "f.txt", "changed\n")
	gitRunDir(t, dir, "", "commit", "-qam", "main commit")
	otherSha := gitOutDir(t, dir, "rev-parse", "main")

	meta := git.VersionMeta{Op: "merge", Ours: oursSha, Other: otherSha, Base: baseSha, Source: "feat/x", Target: "main"}
	syn, err := svc.Repo().WriteVersionSnapshot(ctx, otherSha, meta, 1700000000)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	ref := git.VersionRef("main", "merge", 1700000000)
	if err := svc.Repo().UpdateRef(ctx, ref, syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	stampVersionsFormat(t, dir)

	eps, err := svc.VersionPreview(ctx, ref)
	if err != nil {
		t.Fatalf("VersionPreview: %v", err)
	}
	if eps.Left.Kind() != model.EndpointCommit || eps.Left.Hash() != baseSha {
		t.Errorf("Left = %+v, want commit %s (Base)", eps.Left, baseSha)
	}
	if eps.Right.Kind() != model.EndpointCommit || eps.Right.Hash() != oursSha {
		t.Errorf("Right = %+v, want commit %s (Ours)", eps.Right, oursSha)
	}
	if eps.Left.Hash() == eps.Right.Hash() {
		t.Fatalf("Left and Right must differ: both %s", eps.Left.Hash())
	}
	if eps.Left.Hash() == otherSha || eps.Right.Hash() == otherSha {
		t.Errorf("endpoints leaked Other (%s): %+v", otherSha, eps)
	}
}

// TestVersionPreviewFieldlessRecordReturnsErrNoPreview covers a one-branch op
// (amend, reset, undo-commit, delete-branch, restore): the record has no
// endpoints by design, so VersionPreview must return ErrNoPreview (not an
// error condition) instead of a zero-valued preview, so callers can fall
// back to rendering the commit view.
func TestVersionPreviewFieldlessRecordReturnsErrNoPreview(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	ctx := context.Background()

	tip, err := svc.Repo().RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	meta := git.VersionMeta{Op: "amend"} // one-branch op: every field but Op empty
	syn, err := svc.Repo().WriteVersionSnapshot(ctx, tip, meta, 1700000100)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	ref := git.VersionRef("main", "amend", 1700000100)
	if err := svc.Repo().UpdateRef(ctx, ref, syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	stampVersionsFormat(t, dir)

	_, err = svc.VersionPreview(ctx, ref)
	if !errors.Is(err, ErrNoPreview) {
		t.Fatalf("VersionPreview err = %v, want ErrNoPreview", err)
	}
}

// TestVersionPreviewUnknownRefIsNotFound asserts a well-formed but
// nonexistent version ref returns an error (not a panic).
func TestVersionPreviewUnknownRefIsNotFound(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	ctx := context.Background()
	stampVersionsFormat(t, dir)

	ref := git.VersionRef("main", "rebase", 1700009999)
	_, err := svc.VersionPreview(ctx, ref)
	if err == nil {
		t.Fatal("VersionPreview on an unknown ref: want error, got nil")
	}
	if errors.Is(err, ErrNoPreview) {
		t.Fatalf("VersionPreview on an unknown ref returned ErrNoPreview, want a not-found error: %v", err)
	}
}
