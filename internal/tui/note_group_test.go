package tui

import "testing"

// The group → slot hash lives in domain (domain.GroupSlot, pinned there).

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
