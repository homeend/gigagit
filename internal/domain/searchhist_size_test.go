package domain

import "testing"

func TestEffectiveSearchHistorySize(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want int }{
		{0, DefaultSearchHistorySize},  // unset -> default 20
		{-3, DefaultSearchHistorySize}, // negative -> default
		{5, 5},                         // in range
		{1000, 1000},                   // at ceiling
		{5000, 1000},                   // above ceiling -> clamp
	}
	for _, c := range cases {
		if got := EffectiveSearchHistorySize(c.in); got != c.want {
			t.Fatalf("EffectiveSearchHistorySize(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
