package theme

import "strconv"

// NormalizeColour maps what a user types in the theme editor to the colour
// form ValidColour (and the on-disk config) expects:
//
//	"#rrggbb"            → as is (case preserved)
//	"rrggbb"  (6 hex)    → "#rrggbb"
//	"#rgb"    (4 chars)  → "#rrggbb" (each digit doubled)
//	"rgb"     (3 hex)    → "#rrggbb"
//	"0".."255" digits, leading zeros allowed ("0033", "0208") → the index
//	                       without leading zeros ("33", "208"); "0000" → "0"
//	""                   → "" (unset)
//
// A string of decimal digits is always read as a palette index — even when
// it happens to also be a well-formed 3-char hex triple ("256" is out of
// range and ok=false, never doubled into "#225566") — since a 6+-digit
// decimal string can never be a valid index, that ambiguity only bites the
// 3-char case, and only the letters a-f take a bare 3- or 6-char string down
// the hex path. ok=false for anything else, including a value 7 chars long
// that does not start with '#', anything longer than 7 chars, and an index
// over 255.
func NormalizeColour(s string) (string, bool) {
	if s == "" {
		return "", true
	}
	if len(s) > 7 {
		return "", false
	}
	if s[0] == '#' {
		body := s[1:]
		switch len(body) {
		case 6:
			if isHex(body) {
				return s, true
			}
		case 3:
			if isHex(body) {
				return "#" + doubleHex(body), true
			}
		}
		return "", false
	}
	// A bare 6-char string is unambiguous: no decimal index reaches six
	// digits (the max index, 255, is three), so hex always wins here.
	if len(s) == 6 {
		if isHex(s) {
			return "#" + s, true
		}
		return "", false
	}
	// A bare 3-char string is the one truly ambiguous shape — "256" is both
	// an out-of-range index and a well-formed (if pointless) hex triple.
	// Decimal digits read as the index; only a-f pushes it down the hex path.
	if len(s) == 3 {
		if isDigits(s) {
			return normalizeIndex(s)
		}
		if isHex(s) {
			return "#" + doubleHex(s), true
		}
		return "", false
	}
	if isDigits(s) {
		return normalizeIndex(s)
	}
	return "", false
}

// normalizeIndex parses a run of decimal digits as a 0..255 palette index,
// stripping any leading zeros ("0033" → "33", "0000" → "0").
func normalizeIndex(s string) (string, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 255 {
		return "", false
	}
	return strconv.Itoa(n), true
}

// isHex reports whether every byte of s is an ASCII hex digit.
func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// isDigits reports whether every byte of s is an ASCII decimal digit, and s
// is non-empty.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// doubleHex repeats every byte of a 3-char hex string, preserving case
// ("abc" → "aabbcc", "1A2" → "11AA22").
func doubleHex(s string) string {
	out := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		out = append(out, s[i], s[i])
	}
	return string(out)
}
