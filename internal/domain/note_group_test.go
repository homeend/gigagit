package domain

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestGroupSlotIsStableAndPinned(t *testing.T) {
	t.Parallel()
	if GroupSlot("") != 0 {
		t.Fatal("no group, no slot")
	}
	// Pinned: a change of hash would recolour every user's groups (the TUI's
	// values before the move to domain).
	if GroupSlot(GroupMine) != GroupSlotMine || GroupSlotMine != 5 {
		t.Fatalf("GroupSlot(mine) = %d", GroupSlot(GroupMine))
	}
	for _, id := range []string{"review:r-1", "github:PRR_9", "review:abc"} {
		s := GroupSlot(id)
		if s < 1 || s > 6 {
			t.Fatalf("GroupSlot(%q) = %d", id, s)
		}
		for i := 0; i < 20; i++ {
			if GroupSlot(id) != s {
				t.Fatalf("GroupSlot(%q) is not stable", id)
			}
		}
	}
}

func TestRenderedWireNoteCarriesItsSlot(t *testing.T) {
	t.Parallel()
	r := ResolvedNote{Note: model.Note{ID: "n1", Source: model.NoteSourceUser}, Group: GroupMine}
	if w := ToWireNoteRendered(r, true); w.GroupSlot != GroupSlotMine {
		t.Fatalf("rendered slot = %d", w.GroupSlot)
	}
	if w := ToWireNote(r); w.GroupSlot != 0 {
		t.Fatal("the agent-facing JSON stays unchanged: no group_slot")
	}
}
