package hunkpick

import "testing"

// An Untouched block is the staging pickers' starting state: it resolves to
// its Current lines, yet nothing on it reads as picked.
func TestUntouchedResolvesToCurrentWithNothingPicked(t *testing.T) {
	t.Parallel()
	d := FromDiff([]byte("a\nb\nc\n"), []byte("A\nb\nC\n"))
	d.StartUntouched()
	b := d.Blocks()[0]
	if all, any := b.SideState(Current); all || any {
		t.Fatalf("untouched: the current side must read unpicked, all=%v any=%v", all, any)
	}
	if b.LinePicked(Current, 0) || b.LinePicked(Incoming, 0) {
		t.Fatal("untouched: no line reads as picked")
	}
	if d.Pending() != 0 {
		t.Fatalf("untouched is decided, pending=%d", d.Pending())
	}
	out, ok := d.Resolved()
	if !ok || string(out) != "a\nb\nc\n" {
		t.Fatalf("untouched must resolve to the current side, got %q ok=%v", out, ok)
	}
	if ps, ok := b.ResolvedPicks(); !ok || len(ps) != 1 || ps[0] != (Pick{Side: Current, Line: 0}) {
		t.Fatalf("untouched picks = the current side's lines, got %v ok=%v", ps, ok)
	}
}

// The first pick on an Untouched block starts from nothing: only the picked
// line is in the result, never the current side's lines alongside it.
func TestUntouchedFirstPickStartsEmpty(t *testing.T) {
	t.Parallel()
	d := FromDiff([]byte("a\nb\n"), []byte("A\nb\n"))
	d.StartUntouched()
	b := d.Blocks()[0]
	b.ToggleSide(Incoming)
	if got, _ := b.ResolvedLines(); len(got) != 1 || got[0] != "A" {
		t.Fatalf("picking the incoming side of an untouched block must yield only it, got %v", got)
	}
}

// The master toggle decides an Untouched block that has nothing on the taken
// side (a pure deletion under "take all incoming") as skipped, and clearing
// returns it to Untouched — not to Undecided, which a staging doc never holds.
func TestToggleSideAllOnUntouchedOneSidedBlock(t *testing.T) {
	t.Parallel()
	d := FromDiff([]byte("a\nb\nc\nd\ne\n"), []byte("A\nb\nc\nd\n"))
	d.StartUntouched()
	bs := d.Blocks()
	if len(bs) != 2 || len(bs[1].Incoming) != 0 {
		t.Fatalf("fixture: want a modified hunk and a pure deletion, got %d blocks", len(bs))
	}
	d.ToggleSideAll(Incoming)
	if !bs[1].Skipped() {
		t.Fatalf("take-all-incoming must take the deletion's empty side, mode=%v", bs[1].Mode)
	}
	if out, _ := d.Resolved(); string(out) != "A\nb\nc\nd\n" {
		t.Fatalf("take-all-incoming must yield the incoming file, got %q", out)
	}
	d.ToggleSideAll(Incoming)
	if bs[1].Mode != Untouched {
		t.Fatalf("clearing must return the deletion to untouched, mode=%v", bs[1].Mode)
	}
}
