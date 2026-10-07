package domain

import "hash/fnv"

// GroupSlot is a note group's colour slot (spec 2026-10-07 §1.3): 0 for no
// group, else 1–6 from the group id's FNV-1a hash — stable across runs,
// processes and frontends, so a review and the GitHub review it became
// share a colour in the TUI and in gg web.
func GroupSlot(group string) int {
	if group == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(group))
	return int(h.Sum32()%6) + 1
}

// GroupSlotMine pins "my draft review"'s slot: a hash change would recolour
// every user's groups, and the test catches it.
const GroupSlotMine = 5
