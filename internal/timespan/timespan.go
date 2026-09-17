// Package timespan parses and prints the short human time spans the blame
// view's "recent lines" highlight takes ("7d", "1d 3h 5m", "36h", "90m").
//
// Grammar: a span is one or more tokens, each `<digits><unit>`, optionally
// separated by whitespace, in any order; tokens sum. Units are w (weeks), d
// (days), h (hours) and m (MINUTES — not months: branchfilter.ParseAge is a
// different vocabulary and the two never share a field). A bare number is
// days, so "7" still means what the older "number of days" prompt meant.
//
// DAG leaf: stdlib only. The web has a JS port of both functions
// (filehist.js parseSpan/formatSpan) pinned to Table by a parity test.
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
)

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
		if j == i {
			return 0, ErrSyntax // a unit with no number, or junk
		}
		n, err := strconv.ParseInt(s[i:j], 10, 64)
		if err != nil {
			return 0, errOverflw
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

// Case is one row of Table: the shared Go↔JS conformance table.
type Case struct {
	In        string
	Want      time.Duration
	Formatted string // Format(Want); "" when Err
	Err       bool
}

// Table is the conformance table the JS port must agree with. Keep it small
// and pointed: every grammar rule once, every error class once.
var Table = []Case{
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
	{In: "d", Err: true},
	{In: "1x", Err: true},
	{In: "1.5d", Err: true},
	{In: "1mo", Err: true},
	{In: "60000d", Err: true}, // past the overflow guard (~164 years)
}
