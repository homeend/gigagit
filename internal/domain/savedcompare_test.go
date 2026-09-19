package domain

import (
	"context"
	"errors"
	"testing"
)

// Both shapes store and read back, and an empty right half means a SET.
func TestSavedCompareStoresBothShapes(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()

	pair, err := svc.SavedCompareAdd(ctx, "gg://gigagit@abc1234", "gg://gigagit@def5678", "a pair")
	if err != nil {
		t.Fatalf("SavedCompareAdd pair: %v", err)
	}
	set, err := svc.SavedCompareAdd(ctx, "gg://gigagit@main...feat/x", "", "a set")
	if err != nil {
		t.Fatalf("SavedCompareAdd set: %v", err)
	}
	if pair.ID == set.ID {
		t.Fatal("the pair and the set share an id")
	}
	if pair.IsSet() {
		t.Fatalf("a two-link entry reads as a set: %+v", pair)
	}
	if !set.IsSet() {
		t.Fatalf("a one-link entry reads as a pair: %+v", set)
	}

	got, err := svc.SavedCompareGet(ctx, set.ID)
	if err != nil {
		t.Fatalf("SavedCompareGet: %v", err)
	}
	if got.Right != "" || got.Left != "gg://gigagit@main...feat/x" {
		t.Fatalf("set read back as %+v", got)
	}
	byLabel, err := svc.SavedCompareGet(ctx, "a pair")
	if err != nil || byLabel.ID != pair.ID {
		t.Fatalf("SavedCompareGet by label = %+v, %v", byLabel, err)
	}
	if byLabel.Right != "gg://gigagit@def5678" {
		t.Fatalf("the pair lost its right half: %+v", byLabel)
	}

	if err := svc.SavedCompareRemove(ctx, pair.ID); err != nil {
		t.Fatalf("SavedCompareRemove: %v", err)
	}
	if _, err := svc.SavedCompareGet(ctx, pair.ID); !errors.Is(err, ErrSavedCompareNotFound) {
		t.Fatalf("after Remove = %v, want ErrSavedCompareNotFound", err)
	}
}

// SavedCompareList shows BOTH shapes where PreviewList shows only the merge
// previews. Two arms of ONE fixture, and they must DISAGREE on it.
func TestSavedCompareListShowsBothShapesWherePreviewListShowsOne(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()

	if _, err := svc.PreviewAdd(ctx, "feat/x", "main", "the preview"); err != nil {
		t.Fatalf("PreviewAdd: %v", err)
	}
	if _, err := svc.SavedCompareAdd(ctx, "gg://gigagit@abc1234", "gg://gigagit@def5678", "the comparison"); err != nil {
		t.Fatalf("SavedCompareAdd: %v", err)
	}

	all, err := svc.SavedCompareList(ctx)
	if err != nil {
		t.Fatalf("SavedCompareList: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("SavedCompareList = %d entries, want 2 (both shapes)", len(all))
	}
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		t.Fatalf("PreviewList: %v", err)
	}
	if len(ps) != 1 || ps[0].Label != "the preview" {
		t.Fatalf("PreviewList = %+v, want exactly the merge preview", ps)
	}
}

// A malformed link is refused at the door, so the store never holds a row
// that cannot be read back. Asked of BOTH halves: the right half is the one
// an early return would skip.
func TestSavedCompareAddRefusesAMalformedLink(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	for name, args := range map[string][2]string{
		"left":  {"not-a-link", ""},
		"right": {"gg://gigagit@abc1234", "not-a-link"},
	} {
		if _, err := svc.SavedCompareAdd(ctx, args[0], args[1], "x"); err == nil {
			t.Fatalf("%s: SavedCompareAdd accepted a non-link", name)
		}
	}
	if cs, err := svc.SavedCompareList(ctx); err != nil || len(cs) != 0 {
		t.Fatalf("a refused add still stored something: %+v, %v", cs, err)
	}
}

// A duplicate returns the EXISTING record with ErrSavedCompareExists, so a
// frontend can focus it instead of failing.
func TestSavedCompareAddIsIdempotent(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	first, err := svc.SavedCompareAdd(ctx, "gg://gigagit@abc1234", "gg://gigagit@def5678", "mine")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	again, err := svc.SavedCompareAdd(ctx, "gg://gigagit@abc1234", "gg://gigagit@def5678", "other")
	if !errors.Is(err, ErrSavedCompareExists) {
		t.Fatalf("second err = %v, want ErrSavedCompareExists", err)
	}
	if again.ID != first.ID || again.Label != "mine" {
		t.Fatalf("ErrSavedCompareExists returned %+v, want the stored record", again)
	}
}

// A saved comparison and a merge preview of the SAME left half are two rows,
// and neither surface may delete the other's. PreviewRemove already refuses a
// pair; this is the other direction — SavedCompareRemove owns both, on
// purpose, because it is the surface that shows both.
func TestSavedCompareRemoveOwnsBothShapes(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	p, err := svc.PreviewAdd(ctx, "feat/x", "main", "the preview")
	if err != nil {
		t.Fatalf("PreviewAdd: %v", err)
	}
	if err := svc.SavedCompareRemove(ctx, p.ID); err != nil {
		t.Fatalf("SavedCompareRemove on a preview: %v", err)
	}
	if ps, err := svc.PreviewList(ctx); err != nil || len(ps) != 0 {
		t.Fatalf("the preview survived: %+v, %v", ps, err)
	}
}
