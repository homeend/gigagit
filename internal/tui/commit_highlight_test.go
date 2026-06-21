package tui

import (
	"fmt"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// highlightModel builds a Model with fully-controlled commits: digit-only hashes
// (so a query containing any non-hex letter can only match a subject, never a
// hash) and no refs. Ideal for testing the pure match/scan logic deterministically.
func highlightModel(subjects ...string) Model {
	cs := make([]model.Commit, len(subjects))
	for i, s := range subjects {
		cs[i] = model.Commit{Hash: fmt.Sprintf("%040d", i), Subject: s}
	}
	return Model{commits: cs, sel: map[panel]int{}}
}

func TestCommitMatchesHighlight(t *testing.T) {
	m := highlightModel("row-zero", "row-one", "row-two", "row-three", "row-four", "row-five")
	m.highlightQuery = "three"
	matches := 0
	for i := range m.commits {
		if m.commitMatchesHighlight(i) {
			matches++
		}
	}
	if matches != 1 || !m.commitMatchesHighlight(3) {
		t.Fatalf("query 'three' should match exactly row 3, got %d matches", matches)
	}
	// case-insensitive
	m.highlightQuery = "THREE"
	if !m.commitMatchesHighlight(3) {
		t.Fatal("match must be case-insensitive")
	}
	// empty query matches nothing
	m.highlightQuery = ""
	for i := range m.commits {
		if m.commitMatchesHighlight(i) {
			t.Fatalf("empty query must match nothing (row %d)", i)
		}
	}
}

func TestScanHighlightMatchWrap(t *testing.T) {
	m := highlightModel("row-zero", "row-one", "row-two", "row-three", "row-four", "row-five")
	last := len(m.commits) - 1
	m.highlightQuery = "row" // every row matches

	// exclusive forward from 0 -> 1
	if got, ok := m.scanHighlightMatch(0, +1, false); !ok || got != 1 {
		t.Fatalf("forward from 0 => (%d,%v), want (1,true)", got, ok)
	}
	// exclusive forward from last wraps to 0
	if got, ok := m.scanHighlightMatch(last, +1, false); !ok || got != 0 {
		t.Fatalf("forward wrap from %d => (%d,%v), want (0,true)", last, got, ok)
	}
	// exclusive backward from 0 wraps to last
	if got, ok := m.scanHighlightMatch(0, -1, false); !ok || got != last {
		t.Fatalf("backward wrap from 0 => (%d,%v), want (%d,true)", got, ok, last)
	}
	// inclusive: a matching `from` stays put
	if got, ok := m.scanHighlightMatch(2, +1, true); !ok || got != 2 {
		t.Fatalf("inclusive from matching 2 => (%d,%v), want (2,true)", got, ok)
	}

	// a query matching only row 5; exclusive forward from 0 lands on 5
	m.highlightQuery = "five"
	if got, ok := m.scanHighlightMatch(0, +1, false); !ok || got != last {
		t.Fatalf("'five' forward from 0 => (%d,%v), want (%d,true)", got, ok, last)
	}

	// no-match query returns (from,false)
	m.highlightQuery = "zzz"
	if got, ok := m.scanHighlightMatch(3, +1, false); ok || got != 3 {
		t.Fatalf("no-match => (%d,%v), want (3,false)", got, ok)
	}
}
