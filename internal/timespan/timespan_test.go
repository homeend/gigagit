package timespan

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	t.Parallel()
	const (
		m = time.Minute
		h = time.Hour
		d = 24 * time.Hour
		w = 7 * d
	)
	ok := []struct {
		in   string
		want time.Duration
	}{
		{"7d", 7 * d},
		{"7", 7 * d}, // a bare number is days
		{" 7 ", 7 * d},
		{"1w", w},
		{"36h", 36 * h},
		{"90m", 90 * m},
		{"1d 3h 5m", d + 3*h + 5*m},
		{"1d3h5m", d + 3*h + 5*m},
		{"5m 3h 1d", d + 3*h + 5*m}, // order does not matter
		{"1D 3H", d + 3*h},          // case-insensitive units
		{"2w 1d", 2*w + d},
		{"1d 1d", 2 * d}, // repeats sum
		{"1d 3", 4 * d},  // a trailing bare number is still days
		{"w", w},         // a bare unit is one of it
		{"d 3h", d + 3*h},
		{"H", h},
	}
	for _, c := range ok {
		got, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	bad := []string{"", "   ", "0", "0d", "0h 0m", "x", "1x", "1 x", "1.5d", "-1d", "1d,3h", "1y", "1mo", "dd"}
	for _, in := range bad {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %v, want an error", in, got)
		}
	}
}

func TestFormat(t *testing.T) {
	t.Parallel()
	const (
		m = time.Minute
		h = time.Hour
		d = 24 * time.Hour
	)
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0m"},
		{5 * m, "5m"},
		{3 * h, "3h"},
		{7 * d, "7d"},
		{14 * d, "14d"}, // weeks are an input convenience only: never printed
		{d + 3*h + 5*m, "1d3h5m"},
		{36 * h, "1d12h"},
		{90 * m, "1h30m"},
		{30 * time.Second, "0m"}, // sub-minute rounds down
	}
	for _, c := range cases {
		if got := Format(c.in); got != c.want {
			t.Errorf("Format(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"7d", "1d3h5m", "36h", "90m", "2w"} {
		d, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		back, err := Parse(Format(d))
		if err != nil || back != d {
			t.Errorf("Parse(Format(%q)) = %v, %v; want %v", in, back, err, d)
		}
	}
}

func TestParseFilter(t *testing.T) {
	t.Parallel()
	const (
		h = time.Hour
		d = 24 * time.Hour
	)
	ok := []struct {
		in   string
		want Filter
		str  string
	}{
		{"7d", Filter{Younger: 7 * d, HasYounger: true}, "-7d"},
		{"-7d", Filter{Younger: 7 * d, HasYounger: true}, "-7d"},
		{"- 7d", Filter{Younger: 7 * d, HasYounger: true}, "-7d"},
		{"+30d", Filter{Older: 30 * d, HasOlder: true}, "+30d"},
		{"+w", Filter{Older: 7 * d, HasOlder: true}, "+7d"},
		{"+1d 2h -7d 4h", Filter{Older: d + 2*h, Younger: 7*d + 4*h, HasOlder: true, HasYounger: true}, "+1d2h -7d4h"},
		{"-7d +1d", Filter{Older: d, Younger: 7 * d, HasOlder: true, HasYounger: true}, "+1d -7d"},
		{"+7d -7d", Filter{Older: 7 * d, Younger: 7 * d, HasOlder: true, HasYounger: true}, "+7d -7d"},
		{"+1d 23h -1234h", Filter{Older: 47 * h, Younger: 1234 * h, HasOlder: true, HasYounger: true}, "+1d23h -51d10h"},
	}
	for _, c := range ok {
		got, err := ParseFilter(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseFilter(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
			continue
		}
		if got.String() != c.str {
			t.Errorf("ParseFilter(%q).String() = %q, want %q", c.in, got.String(), c.str)
		}
	}
	bad := []string{"", "+", "-", "+ ", "-7d +", "+7d -1d", "-7d -3d", "+1d +2d", "3d -1d", "+1mo", "+0d", "1d-3h"}
	for _, in := range bad {
		if got, err := ParseFilter(in); err == nil {
			t.Errorf("ParseFilter(%q) = %+v, want an error", in, got)
		}
	}
}

func TestFilterMatches(t *testing.T) {
	t.Parallel()
	const d = 24 * time.Hour
	younger := Filter{Younger: 7 * d, HasYounger: true}
	older := Filter{Older: 7 * d, HasOlder: true}
	between := Filter{Older: d, Younger: 7 * d, HasOlder: true, HasYounger: true}
	cases := []struct {
		f    Filter
		age  time.Duration
		want bool
	}{
		{younger, 0, true}, {younger, 7 * d, true}, {younger, 7*d + 1, false},
		{older, 7 * d, true}, {older, 7*d - 1, false}, {older, 0, false},
		{between, d - 1, false}, {between, d, true}, {between, 3 * d, true}, {between, 7 * d, true}, {between, 7*d + 1, false},
		{younger, -time.Hour, true}, // clock skew clamps to age 0
		{older, -time.Hour, false},
		{Filter{}, 0, false}, // the zero filter matches nothing
	}
	for _, c := range cases {
		if got := c.f.Matches(c.age); got != c.want {
			t.Errorf("%v.Matches(%v) = %v, want %v", c.f, c.age, got, c.want)
		}
	}
	if s := (Filter{}).String(); s != "" {
		t.Errorf("zero Filter.String() = %q", s)
	}
}

// Table is what the web parity test replays through the JS port.
func TestTableAgreesWithParse(t *testing.T) {
	t.Parallel()
	for _, c := range Table {
		if c.IsFilter {
			got, err := ParseFilter(c.In)
			if c.Err {
				if err == nil {
					t.Errorf("Table filter %q: want an error, got %+v", c.In, got)
				}
				continue
			}
			if err != nil || got != c.Filter {
				t.Errorf("Table filter %q: got %+v, %v; want %+v", c.In, got, err, c.Filter)
			}
			if f := got.String(); f != c.Formatted {
				t.Errorf("Table filter %q: String = %q, want %q", c.In, f, c.Formatted)
			}
			continue
		}
		got, err := Parse(c.In)
		if c.Err {
			if err == nil {
				t.Errorf("Table %q: want an error, got %v", c.In, got)
			}
			continue
		}
		if err != nil || got != c.Want {
			t.Errorf("Table %q: got %v, %v; want %v", c.In, got, err, c.Want)
		}
		if f := Format(got); f != c.Formatted {
			t.Errorf("Table %q: Format = %q, want %q", c.In, f, c.Formatted)
		}
	}
}
