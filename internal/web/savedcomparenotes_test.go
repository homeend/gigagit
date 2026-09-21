package web

import (
	"context"
	"net/http"
	"testing"
)

// TestSavedComparesPairRowCarriesItsNoteTotal: the ◆N the TUI paints on a
// pair's Previews row. The fixture's three commits each carry a note and the
// pair is c0..c2, so the total is 2 — c0's note is NOT the pair's. A count of
// 3 (every note in the repo) or 1 (the tip only) would each be a wrong scope.
func TestSavedComparesPairRowCarriesItsNoteTotal(t *testing.T) {
	t.Parallel()
	ts, svc, c := pairNotesRepo(t)
	p, err := svc.PairAdd(context.Background(), c[0], c[2], "the pair")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Entries []struct {
			ID    string `json:"id"`
			Notes int    `json:"notes"`
		} `json:"entries"`
	}
	if code := getAny(t, ts, "/api/saved-compares", &got); code != http.StatusOK || len(got.Entries) != 1 {
		t.Fatalf("code=%d entries=%+v", code, got.Entries)
	}
	if got.Entries[0].ID != p.ID || got.Entries[0].Notes != 2 {
		t.Fatalf("pair row = %+v, want id %s with notes 2 (c1 + c2, never c0)", got.Entries[0], p.ID)
	}
}
