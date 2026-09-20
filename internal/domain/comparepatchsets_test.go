package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The rule is per SIDE: a set survives ComparePatch exactly when re-deriving
// it from its endpoint reproduces it. The table is the one that used to live
// in internal/cli — including the two look-alike shelf rows, which share an
// endpoint and must disagree.
func TestPatchLosesKeySetPerSide(t *testing.T) {
	t.Parallel()
	shelfEP, err := model.ShelfEndpoint("wt-parser-9f3a1")
	if err != nil {
		t.Fatal(err)
	}
	commit := model.Endpoint{}
	for _, c := range []struct {
		name  string
		fs    FileSet
		loses bool
	}{
		{"an unbounded point", FileSet{ep: commit}, false},
		{"a whole shelf entry", FileSet{ep: shelfEP, bounded: true, paths: []string{"a", "b"}}, false},
		{"the same shelf entry, narrowed to a path", FileSet{ep: shelfEP, bounded: true, narrowed: true, paths: []string{"a"}}, true},
		{"a pair (bounded, endpoint = commit b)", FileSet{ep: commit, bounded: true, paths: []string{"a"}}, true},
		{"a file link at a commit (narrowed)", FileSet{ep: commit, bounded: true, narrowed: true, paths: []string{"a"}}, true},
		{"an EMPTY bounded set still loses", FileSet{ep: commit, bounded: true}, true},
	} {
		if got := c.fs.patchLosesKeySet(); got != c.loses {
			t.Errorf("%s: loses = %v, want %v", c.name, got, c.loses)
		}
	}
}

// ComparePatchSets is the door: it refuses BEFORE rendering, and says which
// side. `pair × shelf` is the row that once answered wrongly — asked per pair,
// "a shelf is on one side" let the change-set through.
func TestComparePatchSetsRefusesPerSide(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	shelfEP, err := model.ShelfEndpoint("wt-parser-9f3a1")
	if err != nil {
		t.Fatal(err)
	}
	pair := FileSet{ep: model.Endpoint{}, bounded: true, paths: []string{"a"}}
	shelf := FileSet{ep: shelfEP, bounded: true, paths: []string{"a"}}
	for _, c := range []struct {
		name        string
		left, right FileSet
		l, r        bool
	}{
		{"pair × shelf", pair, shelf, true, false},
		{"shelf × pair", shelf, pair, false, true},
		{"pair × pair", pair, pair, true, true},
	} {
		_, err := svc.ComparePatchSets(context.Background(), c.left, c.right)
		var lost *PatchLosesSetError
		if !errors.As(err, &lost) {
			t.Fatalf("%s: err = %v, want a *PatchLosesSetError", c.name, err)
		}
		if lost.Left != c.l || lost.Right != c.r {
			t.Errorf("%s: sides = %v/%v, want %v/%v", c.name, lost.Left, lost.Right, c.l, c.r)
		}
	}
}
