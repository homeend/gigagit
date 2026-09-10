package hunkpick

import "testing"

// ResolvedPicks must expand to exactly the lines ResolvedLines produces, but
// tagged with the side and line index each came from.
func TestResolvedPicksMatchesResolvedLines(t *testing.T) {
	t.Parallel()
	mk := func() *Block {
		return &Block{Current: []string{"c0", "c1", "c2"}, Incoming: []string{"i0", "i1"}}
	}
	cases := []struct {
		name  string
		setup func(*Block)
	}{
		{"take current", func(b *Block) { b.Mode = TakeCurrent }},
		{"take incoming", func(b *Block) { b.Mode = TakeIncoming }},
		{"line by line", func(b *Block) {
			b.Mode = LineByLine
			b.Picks = []Pick{{Side: Incoming, Line: 1}, {Side: Current, Line: 0}}
		}},
		{"skipped", func(b *Block) { b.Skip() }},
	}
	for _, tc := range cases {
		b := mk()
		tc.setup(b)
		want, wantOK := b.ResolvedLines()
		ps, ok := b.ResolvedPicks()
		if ok != wantOK {
			t.Fatalf("%s: ok = %v, want %v", tc.name, ok, wantOK)
		}
		if len(ps) != len(want) {
			t.Fatalf("%s: %d picks, want %d lines", tc.name, len(ps), len(want))
		}
		for i, p := range ps {
			var got string
			if p.Side == Current {
				got = b.Current[p.Line]
			} else {
				got = b.Incoming[p.Line]
			}
			if got != want[i] {
				t.Errorf("%s: pick %d resolves to %q, want %q", tc.name, i, got, want[i])
			}
		}
	}
}

func TestResolvedPicksUndecidedIsNotOK(t *testing.T) {
	t.Parallel()
	b := &Block{Current: []string{"c0"}, Incoming: []string{"i0"}}
	if ps, ok := b.ResolvedPicks(); ok || ps != nil {
		t.Fatalf("undecided block = (%v, %v), want (nil, false)", ps, ok)
	}
}

// An out-of-range pick (a stale index) is dropped, exactly as resolved() drops it.
func TestResolvedPicksDropsOutOfRangeLines(t *testing.T) {
	t.Parallel()
	b := &Block{Current: []string{"c0"}, Incoming: nil, Mode: LineByLine,
		Picks: []Pick{{Side: Current, Line: 5}, {Side: Current, Line: 0}}}
	ps, ok := b.ResolvedPicks()
	if !ok || len(ps) != 1 || ps[0].Line != 0 {
		t.Fatalf("picks = %v ok=%v, want the single in-range pick", ps, ok)
	}
}
