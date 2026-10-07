package tui

import "testing"

func TestGroupSlotIsStableAndInRange(t *testing.T) {
	t.Parallel()
	if groupSlot("") != 0 {
		t.Fatal("no group, no slot")
	}
	ids := []string{"mine", "review:r-1", "review:r-2", "github:PRR_9", "review:abc", "github:x"}
	for _, id := range ids {
		s := groupSlot(id)
		if s < 1 || s > 6 {
			t.Fatalf("groupSlot(%q) = %d, want 1..6", id, s)
		}
		for i := 0; i < 50; i++ { // no map order, no randomness
			if groupSlot(id) != s {
				t.Fatalf("groupSlot(%q) is not stable", id)
			}
		}
	}
	// Pinned value: a change of hash would recolour every user's groups.
	if got := groupSlot("mine"); got != groupSlotMine {
		t.Fatalf("groupSlot(mine) = %d, want %d", got, groupSlotMine)
	}
}

func TestGroupBarStyleUsesTheSlotRole(t *testing.T) {
	t.Parallel()
	for slot := 1; slot <= 6; slot++ {
		if _, ok := groupBarStyle(slot); !ok {
			t.Fatalf("slot %d has no style", slot)
		}
	}
	if _, ok := groupBarStyle(0); ok {
		t.Fatal("slot 0 is no group: no bar")
	}
}
