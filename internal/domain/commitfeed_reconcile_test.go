package domain

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// cs builds a newest-first commit slice from a space-separated hash list.
func cs(hashes string) []model.Commit {
	if hashes == "" {
		return nil
	}
	parts := strings.Fields(hashes)
	out := make([]model.Commit, 0, len(parts))
	for _, h := range parts {
		out = append(out, model.Commit{Hash: h})
	}
	return out
}

// hashList renders a commit slice back to a space-separated hash list.
func hashList(cm []model.Commit) string {
	hs := make([]string, 0, len(cm))
	for _, c := range cm {
		hs = append(hs, c.Hash)
	}
	return strings.Join(hs, " ")
}

func TestReconcilePage(t *testing.T) {
	cases := []struct {
		name      string
		loaded    string
		page      string
		wantOK    bool
		want      string
		skipDelta int
	}{
		{
			name: "pure prepend keeps the deep tail",
			// Two new commits landed on top; everything paged in survives.
			loaded: "a b c d e", page: "y z a b c",
			wantOK: true, want: "y z a b c d e", skipDelta: 2,
		},
		{
			name:   "unchanged history is a no-op merge",
			loaded: "a b c d e", page: "a b c",
			wantOK: true, want: "a b c d e", skipDelta: 0,
		},
		{
			name: "amended tip replaces the head only",
			// a2 replaces a; b is the anchor, found at loaded index 1.
			loaded: "a b c d e", page: "a2 b c",
			wantOK: true, want: "a2 b c d e", skipDelta: 0,
		},
		{
			name:   "dropped tip (reset --hard HEAD~1) trims the head",
			loaded: "a b c d e", page: "b c d",
			wantOK: true, want: "b c d e", skipDelta: -1,
		},
		{
			name:   "new commits replacing a dropped tip",
			loaded: "a b c d e", page: "y z b c",
			wantOK: true, want: "y z b c d e", skipDelta: 1,
		},
		{
			name: "no overlap falls back",
			// More new commits than the page holds, or a full rewrite.
			loaded: "a b c d e", page: "v w x y z",
			wantOK: false,
		},
		{
			name: "interleaved insert fails the alignment check",
			// z appears between a and b: the shared hash 'a' is not enough.
			loaded: "a b c d e", page: "a z b c",
			wantOK: false,
		},
		{
			name:   "reordered head fails the alignment check",
			loaded: "a b c d e", page: "b a c d",
			wantOK: false,
		},
		{
			name:   "rewritten tail below the anchor fails",
			loaded: "a b c d e", page: "a b c9 d",
			wantOK: false,
		},
		{
			name: "a page covering the whole loaded tail resets instead",
			// Everything still loaded is inside the fresh page, so keeping the
			// old accumulation buys nothing — take the page (simpler skip).
			loaded: "d e", page: "a b c d e f",
			wantOK: false,
		},
		{
			name:   "empty loaded list cannot reconcile",
			loaded: "", page: "a b c",
			wantOK: false,
		},
		{
			name:   "empty page cannot reconcile",
			loaded: "a b c", page: "",
			wantOK: false,
		},
		{
			name: "duplicate hash in the new head is merged once but still counts for skip",
			// git can emit the same commit twice across refs; the merged list must
			// stay unique, while skipDelta tracks git's RAW walk length so the next
			// page offset stays aligned.
			loaded: "a b c", page: "y y a b",
			wantOK: true, want: "y a b c", skipDelta: 2,
		},
		{
			name:   "single-commit overlap at the page tail still reconciles",
			loaded: "c d e", page: "y z c",
			wantOK: true, want: "y z c d e", skipDelta: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged, skipDelta, ok := reconcilePage(cs(tc.loaded), cs(tc.page))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (merged %q)", ok, tc.wantOK, hashList(merged))
			}
			if !ok {
				return
			}
			if got := hashList(merged); got != tc.want {
				t.Fatalf("merged = %q, want %q", got, tc.want)
			}
			if skipDelta != tc.skipDelta {
				t.Fatalf("skipDelta = %d, want %d", skipDelta, tc.skipDelta)
			}
		})
	}
}

// TestReconcilePageDoesNotAliasLoaded guards the merged slice against sharing an
// array with the caller's loaded slice: a later write to one must never land in
// the other.
func TestReconcilePageDoesNotAliasLoaded(t *testing.T) {
	loaded := cs("a b c")
	merged, _, ok := reconcilePage(loaded, cs("z a b"))
	if !ok {
		t.Fatal("expected a reconcile")
	}
	merged[1].Hash = "MUTATED"
	if loaded[0].Hash != "a" {
		t.Fatalf("merged aliases loaded: loaded[0] = %q", loaded[0].Hash)
	}
}
