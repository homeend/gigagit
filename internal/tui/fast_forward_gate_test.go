package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// linear C0(t10) <- C1(t20) <- C2(t30), plus a sibling S(t25) off C0.
func gateFeed() []model.Commit {
	return []model.Commit{
		{Hash: "C2", Parents: []string{"C1"}, UnixTime: 30},
		{Hash: "S", Parents: []string{"C0"}, UnixTime: 25},
		{Hash: "C1", Parents: []string{"C0"}, UnixTime: 20},
		{Hash: "C0", Parents: nil, UnixTime: 10},
	}
}

func TestFeedDescendantAhead(t *testing.T) {
	d, c := feedDescendant(gateFeed(), "C2", "C0")
	if !d || !c {
		t.Fatalf("C2 vs tip C0: got descendant=%v conclusive=%v, want true,true", d, c)
	}
}

func TestFeedDescendantBehind(t *testing.T) {
	d, c := feedDescendant(gateFeed(), "C0", "C2")
	if d || !c {
		t.Fatalf("C0 vs tip C2: got descendant=%v conclusive=%v, want false,true", d, c)
	}
}

func TestFeedDescendantDivergent(t *testing.T) {
	d, c := feedDescendant(gateFeed(), "S", "C1")
	if d || !c {
		t.Fatalf("S vs tip C1: got descendant=%v conclusive=%v, want false,true", d, c)
	}
}

func TestFeedDescendantSelfIsTip(t *testing.T) {
	d, c := feedDescendant(gateFeed(), "C1", "C1")
	if d || !c {
		t.Fatalf("tip itself: got descendant=%v conclusive=%v, want false,true", d, c)
	}
}

func TestFeedDescendantTipNotLoaded(t *testing.T) {
	_, c := feedDescendant(gateFeed(), "C2", "MISSING")
	if c {
		t.Fatalf("tip not loaded must be inconclusive, got conclusive=%v", c)
	}
}

func TestFeedDescendantParentOffWindow(t *testing.T) {
	// C2's parent C1 is NOT loaded → the walk from C2 toward tip C0 is inconclusive.
	feed := []model.Commit{
		{Hash: "C2", Parents: []string{"C1"}, UnixTime: 30},
		{Hash: "C0", Parents: nil, UnixTime: 10},
	}
	_, c := feedDescendant(feed, "C2", "C0")
	if c {
		t.Fatalf("parent off-window must be inconclusive, got conclusive=%v", c)
	}
}
