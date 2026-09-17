// Package timespan parses and prints the short human time spans and age
// filters the blame view's line highlight takes ("7d", "1d 3h 5m", "+1d -7d").
//
// Span grammar: one or more tokens, each `<digits><unit>` (either part may
// be absent, not both), optionally separated by whitespace, in any order;
// tokens sum. Units are w (weeks), d (days), h (hours) and m (MINUTES — not
// months: branchfilter.ParseAge is a different vocabulary and the two never
// share a field). A bare number is days ("7" = 7 days); a bare unit is one
// of it ("w" = 1 week).
//
// Filter grammar: up to two signed halves. "-<span>" = younger than (age ≤
// span), "+<span>" = older than (age ≥ span), both inclusive; a leading
// unsigned span is the "-" half ("7d" ≡ "-7d"). Each sign owns every token
// up to the next sign; whitespace after a sign is fine. Both halves = the
// range between them ("+1d -7d": older than a day, younger than a week).
//
// DAG leaf: stdlib only. The web has a JS port (filehist.js parseFilter /
// formatFilter) pinned to Table by a parity test.
package timespan

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	minute = time.Minute
	hour   = time.Hour
	day    = 24 * time.Hour
	week   = 7 * day
)

var (
	ErrEmpty   = errors.New("timespan: empty")
	ErrZero    = errors.New("timespan: zero span")
	ErrSyntax  = errors.New("timespan: expected <number><w|d|h|m>")
	ErrUnit    = errors.New("timespan: unknown unit (want w, d, h or m)")
	errOverflw = errors.New("timespan: too large")

	ErrDupSign  = errors.New("timespan: a second - or + half")
	ErrEmptyRng = errors.New("timespan: the older bound is above the younger one")
)

// Filter is an age window: Younger caps the age from above (set when
// HasYounger), Older bounds it from below (set when HasOlder). Both
// inclusive. The zero Filter matches nothing (no half set).
type Filter struct {
	Older, Younger       time.Duration
	HasOlder, HasYounger bool
}

// Matches reports whether age falls inside the window: age ≥ Older when
// that half is set, age ≤ Younger when that half is set. A negative age
// (a clock skew: commit time in the future) is clamped to zero, so it
// behaves like an uncommitted line — the youngest thing there is.
func (f Filter) Matches(age time.Duration) bool {
	if !f.HasOlder && !f.HasYounger {
		return false
	}
	if age < 0 {
		age = 0
	}
	if f.HasOlder && age < f.Older {
		return false
	}
	if f.HasYounger && age > f.Younger {
		return false
	}
	return true
}

// String prints the canonical signed form, older half first: "-7d", "+30d",
// "+1d2h -7d4h". Empty for the zero Filter.
func (f Filter) String() string {
	var parts []string
	if f.HasOlder {
		parts = append(parts, "+"+Format(f.Older))
	}
	if f.HasYounger {
		parts = append(parts, "-"+Format(f.Younger))
	}
	return strings.Join(parts, " ")
}

// ParseFilter reads an age filter per the package grammar. Errors: everything
// Parse rejects for a half, a second "-" or "+", a sign with nothing after
// it, and an older bound above the younger one ("+7d -1d"). "+7d -7d" is
// legal (age exactly 7d).
func ParseFilter(s string) (Filter, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Filter{}, ErrEmpty
	}
	var f Filter
	// Split at signs: the text before the first sign (if any) is an unsigned
	// "-" half; each sign then owns the run up to the next sign.
	apply := func(sign byte, text string) error {
		d, err := Parse(text)
		if err != nil {
			return err
		}
		switch sign {
		case '+':
			if f.HasOlder {
				return ErrDupSign
			}
			f.Older, f.HasOlder = d, true
		default:
			if f.HasYounger {
				return ErrDupSign
			}
			f.Younger, f.HasYounger = d, true
		}
		return nil
	}
	sign, start, seenSign := byte('-'), 0, false
	for i := 0; i <= len(s); i++ {
		if i < len(s) && s[i] != '+' && s[i] != '-' {
			continue
		}
		seg := strings.TrimSpace(s[start:i])
		if seg == "" {
			if seenSign {
				return Filter{}, ErrSyntax // a sign with nothing after it
			}
		} else if err := apply(sign, seg); err != nil {
			return Filter{}, err
		}
		if i < len(s) {
			sign, start, seenSign = s[i], i+1, true
		}
	}
	if f.HasOlder && f.HasYounger && f.Older > f.Younger {
		return Filter{}, ErrEmptyRng
	}
	return f, nil
}

// Parse reads a span per the package grammar. Errors: empty input, a token
// without digits, an unknown unit, a zero total.
func Parse(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ErrEmpty
	}
	var total time.Duration
	i := 0
	for i < len(s) {
		// skip whitespace between tokens
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		n := int64(1) // a bare unit ("w", "+d") means one of it
		if j > i {
			var err error
			if n, err = strconv.ParseInt(s[i:j], 10, 64); err != nil {
				return 0, errOverflw
			}
		}
		unit := day // a bare number is days
		if j < len(s) {
			switch s[j] {
			case 'w', 'W':
				unit, j = week, j+1
			case 'd', 'D':
				unit, j = day, j+1
			case 'h', 'H':
				unit, j = hour, j+1
			case 'm', 'M':
				unit, j = minute, j+1
			}
		}
		if j == i {
			return 0, ErrSyntax // neither a number nor a unit: junk
		}
		// The token must end here: whitespace, another number, or the end.
		// "1mo", "1dd", "1.5d", "1x" are junk, not two tokens.
		if j < len(s) && s[j] != ' ' && s[j] != '\t' && !(s[j] >= '0' && s[j] <= '9') {
			return 0, ErrUnit
		}
		if n > int64(1<<62/unit) {
			return 0, errOverflw
		}
		total += time.Duration(n) * unit
		i = j
	}
	if total <= 0 {
		return 0, ErrZero
	}
	return total, nil
}

// Format prints d in the canonical badge form: largest unit first, zero
// parts dropped, no spaces ("1d3h5m"). Weeks are an input convenience only
// and never printed. Sub-minute remainders are dropped; zero prints "0m".
func Format(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := d / day
	d -= days * day
	hours := d / hour
	d -= hours * hour
	mins := d / minute
	var b strings.Builder
	if days > 0 {
		b.WriteString(strconv.FormatInt(int64(days), 10))
		b.WriteByte('d')
	}
	if hours > 0 {
		b.WriteString(strconv.FormatInt(int64(hours), 10))
		b.WriteByte('h')
	}
	if mins > 0 || b.Len() == 0 {
		b.WriteString(strconv.FormatInt(int64(mins), 10))
		b.WriteByte('m')
	}
	return b.String()
}

// Case is one row of Table: the shared Go↔JS conformance table. Filter rows
// (IsFilter) drive ParseFilter/Filter.String; the rest drive Parse/Format.
type Case struct {
	In        string
	Want      time.Duration
	Formatted string // Format(Want) or Filter.String(); "" when Err
	Err       bool
	IsFilter  bool
	Filter    Filter
}

// Table is the conformance table the JS port must agree with. Keep it small
// and pointed: every grammar rule once, every error class once.
var Table = []Case{
	// spans
	{In: "w", Want: week, Formatted: "7d"},
	{In: "d 3h", Want: day + 3*hour, Formatted: "1d3h"},
	{In: "7d", Want: 7 * day, Formatted: "7d"},
	{In: "7", Want: 7 * day, Formatted: "7d"},
	{In: "1w", Want: week, Formatted: "7d"},
	{In: "36h", Want: 36 * hour, Formatted: "1d12h"},
	{In: "90m", Want: 90 * minute, Formatted: "1h30m"},
	{In: "1d 3h 5m", Want: day + 3*hour + 5*minute, Formatted: "1d3h5m"},
	{In: "5m3h1d", Want: day + 3*hour + 5*minute, Formatted: "1d3h5m"},
	{In: " 1D  3H ", Want: day + 3*hour, Formatted: "1d3h"},
	{In: "", Err: true},
	{In: "0", Err: true},
	{In: "x", Err: true},
	{In: "1x", Err: true},
	{In: "1.5d", Err: true},
	{In: "1mo", Err: true},
	{In: "60000d", Err: true}, // past the overflow guard (~164 years)
	// filters
	{In: "7d", IsFilter: true, Filter: Filter{Younger: 7 * day, HasYounger: true}, Formatted: "-7d"},
	{In: "-7w", IsFilter: true, Filter: Filter{Younger: 7 * week, HasYounger: true}, Formatted: "-49d"},
	{In: "+w", IsFilter: true, Filter: Filter{Older: week, HasOlder: true}, Formatted: "+7d"},
	{In: "+ 30d", IsFilter: true, Filter: Filter{Older: 30 * day, HasOlder: true}, Formatted: "+30d"},
	{In: "+1d 2h -7d 4h", IsFilter: true, Filter: Filter{Older: day + 2*hour, Younger: 7*day + 4*hour, HasOlder: true, HasYounger: true}, Formatted: "+1d2h -7d4h"},
	{In: "-7d +1d", IsFilter: true, Filter: Filter{Older: day, Younger: 7 * day, HasOlder: true, HasYounger: true}, Formatted: "+1d -7d"},
	{In: "+1d 23h -1234h", IsFilter: true, Filter: Filter{Older: 47 * hour, Younger: 1234 * hour, HasOlder: true, HasYounger: true}, Formatted: "+1d23h -51d10h"},
	{In: "3d -1d", IsFilter: true, Err: true}, // unsigned + a second younger half
	{In: "+7d -7d", IsFilter: true, Filter: Filter{Older: 7 * day, Younger: 7 * day, HasOlder: true, HasYounger: true}, Formatted: "+7d -7d"},
	{In: "+7d -1d", IsFilter: true, Err: true}, // empty range
	{In: "-7d -3d", IsFilter: true, Err: true}, // duplicate sign
	{In: "+", IsFilter: true, Err: true},       // sign with nothing after it
	{In: "-7d +", IsFilter: true, Err: true},   // trailing bare sign
	{In: "", IsFilter: true, Err: true},
	{In: "+1mo", IsFilter: true, Err: true}, // token error inside a half
}
