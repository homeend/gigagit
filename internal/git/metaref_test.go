package git

import (
	"context"
	"testing"
)

func TestMetaRefRoundTrip(t *testing.T) {
	t.Parallel()
	ref := MetaRef("versions", 2)
	if want := "refs/gg/meta/versions/2"; ref != want {
		t.Fatalf("MetaRef = %q, want %q", ref, want)
	}
	store, format, ok := ParseMetaRef(ref)
	if !ok || store != "versions" || format != 2 {
		t.Errorf("ParseMetaRef(%q) = (%q, %d, %v), want (versions, 2, true)", ref, store, format, ok)
	}
}

func TestParseMetaRefRejectsOtherRefs(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{
		"refs/heads/main",
		"refs/gg/versions/main/1700000000-rebase",
		"refs/gg/meta/versions",   // no format segment
		"refs/gg/meta/versions/x", // non-numeric format
	} {
		if _, _, ok := ParseMetaRef(ref); ok {
			t.Errorf("ParseMetaRef(%q) = ok, want not ok", ref)
		}
	}
}

func TestStampAndReadStoreFormats(t *testing.T) {
	t.Parallel()
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	got, err := r.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats on a fresh repo: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("StoreFormats = %v, want empty", got)
	}

	if err := r.StampStoreFormat(ctx, "versions", 1); err != nil {
		t.Fatalf("StampStoreFormat: %v", err)
	}
	got, err = r.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats: %v", err)
	}
	if got["versions"] != 1 {
		t.Errorf("StoreFormats = %v, want versions=1", got)
	}
}

func TestStampStoreFormatReplacesTheOldMarker(t *testing.T) {
	t.Parallel()
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	if err := r.StampStoreFormat(ctx, "versions", 1); err != nil {
		t.Fatalf("StampStoreFormat(1): %v", err)
	}
	if err := r.StampStoreFormat(ctx, "versions", 2); err != nil {
		t.Fatalf("StampStoreFormat(2): %v", err)
	}

	got, err := r.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats: %v", err)
	}
	if got["versions"] != 2 {
		t.Errorf("StoreFormats = %v, want versions=2", got)
	}
	refs, err := r.ForEachRef(ctx, MetaRefPrefix)
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("got %d marker refs, want exactly 1 (the old one must be removed)", len(refs))
	}
}
