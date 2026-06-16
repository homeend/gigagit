package hunkpick

import (
	"bytes"
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
