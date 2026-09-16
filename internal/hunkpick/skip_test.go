package hunkpick

import "testing"

func TestSkipResolvesWithNothing(t *testing.T) {
	t.Parallel()
	d, _ := ParseConflict([]byte("top\n<<<<<<< HEAD\nfoo\n=======\nbar\n>>>>>>> x\nend\n"))
	b := d.Blocks()[0]
	if b.Skipped() {
		t.Fatal("fresh block must not read as skipped")
	}
	b.Skip()
	if !b.Skipped() || d.Pending() != 0 {
		t.Fatalf("skip must decide the block: skipped=%v pending=%d", b.Skipped(), d.Pending())
	}
	out, ok := d.Resolved()
	if !ok || string(out) != "top\nend\n" {
		t.Fatalf("skipped region must contribute nothing: %q ok=%v", out, ok)
	}
	b.Unskip()
	if b.Mode != Undecided || d.Pending() != 1 {
		t.Fatal("unskip must return the block to Undecided")
	}
	b.ToggleSide(Current)
	b.Unskip() // not skipped: no-op
	if b.Mode == Undecided {
		t.Fatal("unskip must not touch a block with picks")
	}
}

// A region with nothing on the chosen side has nothing to offer a master
// "take current"/"take incoming": the completing pass marks it skipped so the
// document is fully decided, and the clearing pass returns it to Undecided
// (together with everything else it unpicks). Regions the user already
// decided are never overwritten on the completing pass.
func TestToggleSideAllSkipsEmptySideBlocks(t *testing.T) {
	t.Parallel()
	d, _ := ParseConflict([]byte(
		"top\n<<<<<<< HEAD\na\n=======\nx\n>>>>>>> r\nmid\n<<<<<<< HEAD\nb\n=======\ny\n>>>>>>> r\n" +
			"<<<<<<< HEAD\n=======\nz\n>>>>>>> r\nend\n"))
	blocks := d.Blocks()
	if len(blocks) != 3 || len(blocks[2].Current) != 0 {
		t.Fatalf("fixture: want 3 blocks, third with an empty current side; got %d", len(blocks))
	}
	blocks[1].ToggleSide(Incoming) // user's own decision on block 1: y only

	d.ToggleSideAll(Current) // completing pass
	if !blocks[2].Skipped() {
		t.Fatal("empty-current block must be marked skipped by C")
	}
	if d.Pending() != 0 {
		t.Fatalf("Pending = %d after C, want 0", d.Pending())
	}
	out, ok := d.Resolved()
	if !ok || string(out) != "top\na\nmid\ny\nb\nend\n" {
		t.Fatalf("resolved = %q ok=%v", out, ok)
	}

	d.ToggleSideAll(Current) // everything full on current → clearing pass
	if blocks[2].Mode != Undecided {
		t.Fatalf("clearing pass must reset the auto-skipped block to Undecided, got mode %d skipped=%v", blocks[2].Mode, blocks[2].Skipped())
	}
	if _, any := d.SideStateAll(Current); any {
		t.Fatal("clearing pass must unpick current everywhere")
	}
	if d.Pending() != 1 {
		t.Fatalf("Pending = %d after clearing, want 1 (only the empty-side block)", d.Pending())
	}
}

// The clearing pass leaves a user-skipped block with lines on the chosen side
// alone: it has no picks of that side to clear, and it was not the master
// toggle's doing.
func TestToggleSideAllClearKeepsUserSkipWithLines(t *testing.T) {
	t.Parallel()
	d, _ := ParseConflict([]byte(
		"<<<<<<< HEAD\na\n=======\nx\n>>>>>>> r\n<<<<<<< HEAD\nb\n=======\ny\n>>>>>>> r\n"))
	blocks := d.Blocks()
	blocks[0].ToggleSide(Current)
	blocks[1].Skip()
	d.ToggleSideAll(Current) // block 1 has current lines but is not full → completing pass picks it
	if all, _ := blocks[1].SideState(Current); !all {
		t.Fatal("completing pass picks current on a skipped block that has current lines")
	}
	d.ToggleSideAll(Current) // clearing pass
	if !blocks[0].Skipped() || !blocks[1].Skipped() {
		t.Fatal("clearing pass leaves blocks with lines decided-empty, as before")
	}
}
