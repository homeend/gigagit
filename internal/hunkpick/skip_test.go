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
