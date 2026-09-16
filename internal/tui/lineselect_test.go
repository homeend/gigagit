package tui

import "testing"

// The state machine, in the order the keys fire it: space starts, space again
// freezes, space a third time starts over. bounds() reads the LIVE cursor
// while the range is loose and the frozen end once it is fixed.
func TestLineSelStateMachine(t *testing.T) {
	t.Parallel()
	var s lineSel
	if _, _, ok := s.bounds(5); ok {
		t.Fatal("the zero value must report no selection")
	}
	if s.contains(5, 5) {
		t.Fatal("the zero value contains nothing")
	}

	s.press(10) // first space: anchor at 10, range follows the cursor
	if !s.on || s.fixed || s.anchor != 10 {
		t.Fatalf("after the first press: %+v", s)
	}
	lo, hi, ok := s.bounds(14)
	if !ok || lo != 10 || hi != 14 {
		t.Fatalf("loose bounds at cursor 14 = %d..%d ok=%v, want 10..14", lo, hi, ok)
	}
	// The cursor above the anchor: the range is still anchor..cursor, ordered.
	if lo, hi, _ = s.bounds(3); lo != 3 || hi != 10 {
		t.Fatalf("loose bounds at cursor 3 = %d..%d, want 3..10", lo, hi)
	}

	s.press(14) // second space: the end freezes, the cursor is free
	if !s.fixed || s.end != 14 {
		t.Fatalf("after the second press: %+v", s)
	}
	if lo, hi, _ = s.bounds(99); lo != 10 || hi != 14 {
		t.Fatalf("a frozen range must ignore the cursor: %d..%d, want 10..14", lo, hi)
	}
	if !s.contains(12, 99) || s.contains(15, 99) {
		t.Fatalf("contains is inclusive over the frozen range: %+v", s)
	}

	s.press(20) // third space: a brand-new range at the cursor
	if !s.on || s.fixed || s.anchor != 20 {
		t.Fatalf("after the third press: %+v", s)
	}

	s.clear()
	if s != (lineSel{}) {
		t.Fatalf("clear must restore the zero value, got %+v", s)
	}
}

// A one-space selection copies anchor..cursor, the way tmux does; mark() on a
// cleared selection is a no-op (no host can reach it, but the type must not
// invent an `on` out of a stray call).
func TestLineSelOneSpaceAndInertMark(t *testing.T) {
	t.Parallel()
	var s lineSel
	s.start(7)
	if lo, hi, ok := s.bounds(7); !ok || lo != 7 || hi != 7 {
		t.Fatalf("a one-line selection = %d..%d ok=%v, want 7..7", lo, hi, ok)
	}

	var inert lineSel
	inert.mark(3)
	if inert.on {
		t.Fatalf("mark on a cleared selection must stay off: %+v", inert)
	}
}

// Ordering: the anchor may be BELOW or ABOVE the frozen end, and bounds must
// hand back a low..high pair either way.
func TestLineSelBoundsAreOrdered(t *testing.T) {
	t.Parallel()
	var s lineSel
	s.start(9)
	s.mark(2)
	lo, hi, ok := s.bounds(0)
	if !ok || lo != 2 || hi != 9 {
		t.Fatalf("bounds = %d..%d ok=%v, want 2..9", lo, hi, ok)
	}
	if !s.contains(2, 0) || !s.contains(9, 0) || s.contains(1, 0) || s.contains(10, 0) {
		t.Fatal("contains must be inclusive at both ends and exclusive outside")
	}
}
