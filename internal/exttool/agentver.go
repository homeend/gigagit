package exttool

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is an agent's own version, major.minor.patch (a missing patch is 0).
type Version [3]int

// ParseVersion reads "X.Y" or "X.Y.Z" (decimal components only).
func ParseVersion(s string) (Version, bool) {
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Version{}, false
	}
	var v Version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, false
		}
		v[i] = n
	}
	return v, true
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

func (v Version) less(o Version) bool {
	for i := range v {
		if v[i] != o[i] {
			return v[i] < o[i]
		}
	}
	return false
}

// Range is a half-open agent-version range: [Min, Max). The zero Range
// ("" in the catalog) holds every version.
type Range struct {
	Min, Max       Version
	HasMin, HasMax bool
	src            string
}

// ParseRange reads a space-separated AND of at most one ">=X.Y[.Z]" and at
// most one "<X.Y[.Z]"; "" is every version.
func ParseRange(s string) (Range, error) {
	r := Range{src: strings.TrimSpace(s)}
	for _, f := range strings.Fields(s) {
		var op, rest string
		switch {
		case strings.HasPrefix(f, ">="):
			op, rest = ">=", f[2:]
		case strings.HasPrefix(f, "<") && !strings.HasPrefix(f, "<="):
			op, rest = "<", f[1:]
		default:
			return Range{}, fmt.Errorf("exttool: range %q: want >=X.Y or <X.Y, got %q", s, f)
		}
		v, ok := ParseVersion(rest)
		if !ok {
			return Range{}, fmt.Errorf("exttool: range %q: bad version %q", s, rest)
		}
		if op == ">=" {
			if r.HasMin {
				return Range{}, fmt.Errorf("exttool: range %q: two lower bounds", s)
			}
			r.Min, r.HasMin = v, true
		} else {
			if r.HasMax {
				return Range{}, fmt.Errorf("exttool: range %q: two upper bounds", s)
			}
			r.Max, r.HasMax = v, true
		}
	}
	return r, nil
}

// String is the range as written in the catalog ("" = any).
func (r Range) String() string { return r.src }

// Contains reports Min <= v < Max.
func (r Range) Contains(v Version) bool {
	if r.HasMin && v.less(r.Min) {
		return false
	}
	if r.HasMax && !v.less(r.Max) {
		return false
	}
	return true
}

// Overlaps reports whether some version lies in both half-open ranges.
func (r Range) Overlaps(o Range) bool {
	// r lies wholly below o, or o wholly below r.
	if r.HasMax && o.HasMin && !o.Min.less(r.Max) {
		return false
	}
	if o.HasMax && r.HasMin && !r.Min.less(o.Max) {
		return false
	}
	return true
}

var defaultVersionRe = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)

// ExtractVersion finds the first version in an agent's --version output; re's
// first capture group names it (nil = the first X.Y[.Z] anywhere).
func ExtractVersion(out []byte, re *regexp.Regexp) (Version, bool) {
	if re == nil {
		re = defaultVersionRe
	}
	m := re.FindSubmatch(out)
	if len(m) < 2 {
		return Version{}, false
	}
	return ParseVersion(string(m[1]))
}
