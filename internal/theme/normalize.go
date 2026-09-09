package theme

import "strconv"

// NormalizeColour maps what a user types in the theme editor to the colour
// form ValidColour (and the on-disk config) expects:
//
//	"#rrggbb"             → as is (case preserved)
//	"rrggbb"  (6 chars)   → "#rrggbb" — ALWAYS hex, even when every character
//	                        happens to be a decimal digit ("112233", "000208")
//	"#rgb"    (4 chars)   → "#rrggbb" (each digit doubled)
//	"rgb"     (3 hex)     → "#rrggbb"
//	1-4 decimal digits, leading zeros allowed ("0033", "0208") → the index
//	                        without leading zeros ("33", "208"); "0000" → "0";
//	                        value must be 0..255
//	""                    → "" (unset)
//
// The palette-index form is capped at 4 digits, which removes any ambiguity
// with the 6-character hex form (a bare 6-character string is always hex,
// full stop). The one shape that stays genuinely ambiguous is exactly 3
// characters, where a decimal-only string reads as the index — "256" is
// out of range and ok=false, never doubled into "#225566" — and only a-f
// pushes it down the hex path instead. A 5-digit (or 7-digit, since 7 chars
// without '#' is never a valid shape at all) all-numeric string has no valid
// reading and is ok=false, even though it "fits" if read as an index value.
// ok=false for anything else too, including a value 7 chars long that does
// not start with '#', and anything longer than 7 chars.
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
	switch len(s) {
	case 6:
		// Always hex, even when every character is a decimal digit — the
		// index form never reaches six digits (see below).
		if isHex(s) {
			return "#" + s, true
		}
		return "", false
	case 3:
		// The one truly ambiguous shape — "256" is both an out-of-range
		// index and a well-formed (if pointless) hex triple. Decimal digits
		// read as the index; only a-f pushes it down the hex path.
		if isDigits(s) {
			return normalizeIndex(s)
		}
		if isHex(s) {
			return "#" + doubleHex(s), true
		}
		return "", false
	case 1, 2, 4:
		// The palette-index form: 1-4 decimal digits.
		if isDigits(s) {
			return normalizeIndex(s)
		}
		return "", false
	default:
		// 5 or 7 characters, no '#': no accepted shape reaches here (the
		// index form stops at 4 digits, and hex bare forms are 3 or 6).
		return "", false
	}
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
