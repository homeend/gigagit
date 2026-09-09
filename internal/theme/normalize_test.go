package theme

import "testing"

func TestNormalizeColour(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		// unset
		{"", "", true},

		// "#rrggbb" as-is, case preserved
		{"#123456", "#123456", true},
		{"#ABCDEF", "#ABCDEF", true},
		{"#aAbBcC", "#aAbBcC", true},

		// bare 6 hex → "#rrggbb"
		{"123456", "#123456", true},
		{"abcdef", "#abcdef", true},
		{"ABCDEF", "#ABCDEF", true},

		// "#rgb" (4 chars) → doubled
		{"#abc", "#aabbcc", true},
		{"#1a2", "#11aa22", true},
		{"#ABC", "#AABBCC", true},

		// bare "rgb" (3 hex, at least one non-decimal digit so it is not read
		// as an index) → doubled
		{"abc", "#aabbcc", true},
		{"a1b", "#aa11bb", true},
		{"ABC", "#AABBCC", true},

		// palette index: 1-4 decimal digits, leading zeros allowed
		{"0", "0", true},
		{"9", "9", true},
		{"33", "33", true},
		{"255", "255", true},
		{"0033", "33", true},
		{"0208", "208", true},
		{"0000", "0", true},
		{"0255", "255", true},

		// a bare 6-character string is ALWAYS read as hex, even when every
		// character happens to also be a decimal digit — the index form is
		// capped at 4 digits, so there is no ambiguity to resolve here.
		{"000208", "#000208", true},
		{"112233", "#112233", true},

		// invalid
		{"#GGGGGG", "", false},
		{"12345678", "", false}, // too long, and reads as an out-of-range index
		{"#ff5f00zz", "", false},
		{"256", "", false}, // decimal digits: read as an index, out of range
		{"999", "", false},
		{"#gggg", "", false},    // not a valid "#rgb"
		{"#12", "", false},      // 3 chars, not 4 or 7
		{"#1234567", "", false}, // 8 chars, too long
		{"gg", "", false},
		{"1g2", "", false},
		{"00000255", "", false}, // 8 chars: too long, even though it would parse to 255
		{"00208", "", false},    // 5-digit all-numeric: neither a valid index (>4 digits) nor hex (wrong length)
	}
	for _, c := range cases {
		got, ok := NormalizeColour(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("NormalizeColour(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

// A normalised value must always satisfy ValidColour, except the "" (unset)
// case, which ValidColour deliberately rejects (callers treat "" as "not set"
// before ever asking).
func TestNormalizeColourProducesValidColours(t *testing.T) {
	for _, in := range []string{"#123456", "123456", "#abc", "abc", "0", "255", "0033"} {
		v, ok := NormalizeColour(in)
		if !ok {
			t.Fatalf("NormalizeColour(%q) unexpectedly failed", in)
		}
		if !ValidColour(v) {
			t.Errorf("NormalizeColour(%q) = %q, which ValidColour rejects", in, v)
		}
	}
}
