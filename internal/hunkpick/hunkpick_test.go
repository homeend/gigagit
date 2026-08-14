package hunkpick

import (
	"bytes"
	"strings"
	"testing"
)

func block(cur, inc []string) *Block { return &Block{Current: cur, Incoming: inc} }

func docOf(items ...Item) *Doc { return &Doc{Items: items, FinalNewline: true} }

func TestResolvedTakeSides(t *testing.T) {
	b := block([]string{"a"}, []string{"b"})
	b.Mode = TakeIncoming
	d := docOf(Item{Literal: []string{"top"}}, Item{Block: b}, Item{Literal: []string{"end"}})
	out, ok := d.Resolved()
	if !ok {
		t.Fatal("Resolved ok=false, want true")
	}
	if string(out) != "top\nb\nend\n" {
		t.Fatalf("Resolved = %q", out)
	}
}

func TestResolvedUndecidedBlocksCompletion(t *testing.T) {
	d := docOf(Item{Block: block([]string{"a"}, []string{"b"})})
	if _, ok := d.Resolved(); ok {
		t.Fatal("undecided block must make Resolved ok=false")
	}
	if d.Pending() != 1 {
		t.Fatalf("Pending = %d, want 1", d.Pending())
	}
}

func TestLineByLinePickOrder(t *testing.T) {
	b := block([]string{"foo", "log"}, []string{"bar", "log"})
	b.Mode = LineByLine
	// toggle incoming:bar, then current:foo, then incoming:log → result in that order
	b.ToggleLine(Incoming, 0)
	b.ToggleLine(Current, 0)
	b.ToggleLine(Incoming, 1)
	d := docOf(Item{Block: b})
	out, ok := d.Resolved()
	if !ok {
		t.Fatal("ok=false")
	}
	if string(out) != "bar\nfoo\nlog\n" {
		t.Fatalf("line-by-line order = %q, want bar/foo/log", out)
	}
}

func TestToggleLineRemovesPreservingOrder(t *testing.T) {
	b := block([]string{"x", "y"}, nil)
	b.Mode = LineByLine
	b.ToggleLine(Current, 0) // x
	b.ToggleLine(Current, 1) // y
	b.ToggleLine(Current, 0) // remove x → only y left
	if b.Picked(Current, 0) {
		t.Fatal("current:0 should be unpicked")
	}
	if !b.Picked(Current, 1) {
		t.Fatal("current:1 should remain picked")
	}
	out, _ := docOf(Item{Block: b}).Resolved()
	if !bytes.Equal(out, []byte("y\n")) {
		t.Fatalf("Resolved = %q, want y", out)
	}
}

func TestSetAll(t *testing.T) {
	d := docOf(
		Item{Block: block([]string{"a"}, []string{"b"})},
		Item{Literal: []string{"mid"}},
		Item{Block: block([]string{"c"}, []string{"d"})},
	)
	d.SetAll(TakeCurrent)
	out, ok := d.Resolved()
	if !ok || string(out) != "a\nmid\nc\n" {
		t.Fatalf("SetAll(current) → %q ok=%v", out, ok)
	}
}

func TestNoFinalNewlinePreserved(t *testing.T) {
	d := &Doc{Items: []Item{{Literal: []string{"only"}}}, FinalNewline: false}
	out, _ := d.Resolved()
	if string(out) != "only" {
		t.Fatalf("no-final-newline = %q, want %q", out, "only")
	}
}

func twoSideBlock() *Block {
	return &Block{Current: []string{"c1", "c2"}, Incoming: []string{"i1"}}
}

func TestEnsurePicksMaterializesLegacyModes(t *testing.T) {
	b := twoSideBlock()
	b.Mode = TakeCurrent
	b.EnsurePicks()
	if b.Mode != LineByLine || len(b.Picks) != 2 || !b.Picked(Current, 0) || !b.Picked(Current, 1) {
		t.Fatalf("TakeCurrent should materialize as full current picks: %+v", b.Picks)
	}
	b = twoSideBlock()
	b.Mode = TakeIncoming
	b.EnsurePicks()
	if len(b.Picks) != 1 || !b.Picked(Incoming, 0) {
		t.Fatalf("TakeIncoming should materialize as full incoming picks: %+v", b.Picks)
	}
	b = twoSideBlock() // Undecided
	b.EnsurePicks()
	if b.Mode != LineByLine || len(b.Picks) != 0 {
		t.Fatalf("Undecided should become touched-empty: mode=%v picks=%v", b.Mode, b.Picks)
	}
}

func TestToggleSideTriState(t *testing.T) {
	b := twoSideBlock()
	b.ToggleSide(Current) // complete
	if all, _ := b.SideState(Current); !all {
		t.Fatal("first toggle should pick all current lines")
	}
	b.ToggleSide(Incoming) // both on; incoming appended after current
	out, _ := b.ResolvedLines()
	if strings.Join(out, ",") != "c1,c2,i1" {
		t.Fatalf("both-on order = %v, want current then incoming", out)
	}
	b.ToggleSide(Current) // clear current, incoming order kept
	out, _ = b.ResolvedLines()
	if strings.Join(out, ",") != "i1" {
		t.Fatalf("after clearing current = %v, want just incoming", out)
	}
	// partial → toggle completes (does not clear)
	b = twoSideBlock()
	b.EnsurePicks()
	b.ToggleLine(Current, 1)
	b.ToggleSide(Current)
	out, _ = b.ResolvedLines()
	if strings.Join(out, ",") != "c2,c1" {
		t.Fatalf("completing a partial side = %v, want c2 first (pick order)", out)
	}
}

func TestToggleSideOrderReversed(t *testing.T) {
	b := twoSideBlock()
	b.ToggleSide(Incoming)
	b.ToggleSide(Current)
	out, _ := b.ResolvedLines()
	if strings.Join(out, ",") != "i1,c1,c2" {
		t.Fatalf("i-then-c order = %v, want incoming first", out)
	}
}

func TestToggleSideEmptySideNoOp(t *testing.T) {
	b := &Block{Current: nil, Incoming: []string{"i1"}}
	b.ToggleSide(Current)
	if b.Mode != Undecided {
		t.Fatal("toggling a zero-line side must not touch the block")
	}
	if all, any := b.SideState(Current); all || any {
		t.Fatal("a zero-line side never reads as picked")
	}
}

func TestSideStateLegacyAndLineByLine(t *testing.T) {
	b := twoSideBlock()
	b.Mode = TakeCurrent
	if all, any := b.SideState(Current); !all || !any {
		t.Fatal("TakeCurrent reads as all-current")
	}
	if all, any := b.SideState(Incoming); all || any {
		t.Fatal("TakeCurrent reads as no-incoming")
	}
	if !b.LinePicked(Current, 1) || b.LinePicked(Incoming, 0) {
		t.Fatal("LinePicked must read legacy modes")
	}
	b = twoSideBlock()
	b.EnsurePicks()
	b.ToggleLine(Current, 0)
	if all, any := b.SideState(Current); all || !any {
		t.Fatal("one of two picked = some")
	}
}

func TestToggleSideAllAndSideStateAll(t *testing.T) {
	d := &Doc{Items: []Item{
		{Block: &Block{Current: []string{"a"}, Incoming: []string{"x"}}},
		{Literal: []string{"mid"}},
		{Block: &Block{Current: []string{"b"}, Incoming: []string{"y"}}},
	}}
	d.ToggleSideAll(Current)
	if all, any := d.SideStateAll(Current); !all || !any {
		t.Fatal("master toggle should complete current everywhere")
	}
	if d.Pending() != 0 {
		t.Fatal("master toggle touches every block with that side")
	}
	// partial: clear one block, master completes instead of clearing
	d.Blocks()[0].ToggleSide(Current)
	if all, any := d.SideStateAll(Current); all || !any {
		t.Fatal("mixed state = some")
	}
	d.ToggleSideAll(Current)
	if all, _ := d.SideStateAll(Current); !all {
		t.Fatal("master on a partial state completes")
	}
	d.ToggleSideAll(Current) // now everything full → clears
	if _, any := d.SideStateAll(Current); any {
		t.Fatal("master on a full state clears")
	}
	// cleared blocks stay touched: decided-empty, Resolved drops them
	// (no FinalNewline on this hand-built doc, so no trailing \n)
	out, ok := d.Resolved()
	if !ok || string(out) != "mid" {
		t.Fatalf("decided-empty resolve = %q ok=%v, want just the literal", out, ok)
	}
}

func TestResolvedLinesUndecided(t *testing.T) {
	b := twoSideBlock()
	if _, ok := b.ResolvedLines(); ok {
		t.Fatal("undecided block must report ok=false")
	}
}

func TestLinePickedZeroLineSide(t *testing.T) {
	// LinePicked must guard zero-line sides in legacy modes. TakeCurrent/TakeIncoming
	// would unconditionally return true without checking the side's length.
	// Test both directions: pure-incoming and pure-current blocks.

	// Pure-incoming: Current empty, Mode=TakeCurrent
	b := block(nil, []string{"a"})
	b.Mode = TakeCurrent
	if b.LinePicked(Current, 0) {
		t.Fatalf("LinePicked(Current, 0) on empty Current in TakeCurrent mode must be false")
	}

	// Pure-current: Incoming empty, Mode=TakeIncoming
	b = block([]string{"a"}, nil)
	b.Mode = TakeIncoming
	if b.LinePicked(Incoming, 0) {
		t.Fatalf("LinePicked(Incoming, 0) on empty Incoming in TakeIncoming mode must be false")
	}
}

func TestToggleSideClearPreservesInterleavedOrder(t *testing.T) {
	// space-pick i0, c0, i1 then ToggleSide(Current) clears ONLY the current
	// picks; the incoming picks keep their original relative order.
	b := block([]string{"c0"}, []string{"i0", "i1"})
	b.EnsurePicks()
	b.ToggleLine(Incoming, 0)
	b.ToggleLine(Current, 0)
	b.ToggleLine(Incoming, 1)
	b.ToggleSide(Current) // all current picked → clears the current side
	got, ok := b.ResolvedLines()
	if !ok || len(got) != 2 || got[0] != "i0" || got[1] != "i1" {
		t.Fatalf("after clearing current: lines=%v ok=%v, want [i0 i1]", got, ok)
	}
}
