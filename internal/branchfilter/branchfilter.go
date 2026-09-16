// Package branchfilter is the one evaluator behind gg's five branch-filter
// slots: a named rule that hides, or shows only, branches by tip age and
// name. It is a DAG leaf (stdlib only) so config can decode into Slot, domain
// can compile, and both the TUI and the web server evaluate the SAME matcher —
// a regex accepted here is accepted everywhere (Go RE2), never "works in the
// browser, inert in the terminal".
package branchfilter

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Mode says what a match does.
type Mode string

const (
	ModeHide Mode = "hide" // matching branches are hidden
	ModeShow Mode = "show" // ONLY matching branches are shown
)

// MaxSlots is how many numbered slots exist (alt+1 … alt+5).
const MaxSlots = 5

// Slot is one [[branches.filter]] block as written. Age fields keep the
// user's text ("90d") so config decodes straight into this type; Compile
// parses them. Every set clause must hold (AND) for a branch to match.
type Slot struct {
	Slot        int    `toml:"slot" json:"slot"`
	Name        string `toml:"name" json:"name"`
	Mode        Mode   `toml:"mode" json:"mode"`
	OlderThan   string `toml:"older_than" json:"older_than"`
	YoungerThan string `toml:"younger_than" json:"younger_than"`
	Prefix      string `toml:"prefix" json:"prefix"`
	Suffix      string `toml:"suffix" json:"suffix"`
	Contains    string `toml:"contains" json:"contains"`
	Regex       string `toml:"regex" json:"regex"`
}

// Compiled is a Slot ready to evaluate. Err != nil means the block is inert
// (never an error for the caller — the list simply stays unfiltered and the
// reason is shown). Empty means no clause is set: valid, but matches nothing.
type Compiled struct {
	Slot
	Err   error
	Empty bool

	older, younger time.Duration
	re             *regexp.Regexp
}

// Usable reports whether activating this slot filters anything.
func (c Compiled) Usable() bool { return c.Err == nil && !c.Empty }

// Label is the display name: Name, or "slot N".
func (c Compiled) Label() string {
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	return "slot " + strconv.Itoa(c.Slot.Slot)
}

// Summary renders the rule in one line for Settings rows and chip menus:
// "hide · older than 90d, prefix feat/". Inert slots render their reason.
func (c Compiled) Summary() string {
	if c.Err != nil {
		return "invalid — " + c.Err.Error()
	}
	if c.Empty {
		return "empty (no clause set)"
	}
	var parts []string
	if c.OlderThan != "" {
		parts = append(parts, "older than "+strings.TrimSpace(c.OlderThan))
	}
	if c.YoungerThan != "" {
		parts = append(parts, "younger than "+strings.TrimSpace(c.YoungerThan))
	}
	if c.Prefix != "" {
		parts = append(parts, "prefix "+c.Prefix)
	}
	if c.Suffix != "" {
		parts = append(parts, "suffix "+c.Suffix)
	}
	if c.Contains != "" {
		parts = append(parts, "contains "+c.Contains)
	}
	if c.Regex != "" {
		parts = append(parts, "regex "+c.Regex)
	}
	mode := "hide"
	if c.Mode == ModeShow {
		mode = "show only"
	}
	return mode + " · " + strings.Join(parts, ", ")
}

// ageUnits is the accepted suffix set. Months and years are calendar-free
// approximations on purpose: "6m" is a filter threshold, not a date.
var ageUnits = map[byte]time.Duration{
	'd': 24 * time.Hour,
	'w': 7 * 24 * time.Hour,
	'm': 30 * 24 * time.Hour,
	'y': 365 * 24 * time.Hour,
}

// ParseAge parses "<n><unit>" with unit d|w|m|y and n a positive integer.
func ParseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, errors.New("age must be a number followed by d, w, m or y (e.g. 90d)")
	}
	unit, ok := ageUnits[s[len(s)-1]]
	if !ok {
		return 0, errors.New("age unit must be d, w, m or y (e.g. 90d)")
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, errors.New("age must be a positive whole number of d, w, m or y (e.g. 90d)")
	}
	return time.Duration(n) * unit, nil
}

// FormatAge renders d in the largest unit that divides it exactly.
func FormatAge(d time.Duration) string {
	for _, u := range []struct {
		s byte
		d time.Duration
	}{{'y', ageUnits['y']}, {'m', ageUnits['m']}, {'w', ageUnits['w']}, {'d', ageUnits['d']}} {
		if d > 0 && d%u.d == 0 {
			return strconv.Itoa(int(d/u.d)) + string(u.s)
		}
	}
	return d.String()
}

// Compile validates s. It never panics and never returns an error value —
// the failure travels inside Compiled.Err so the caller can show it.
func Compile(s Slot) Compiled {
	c := Compiled{Slot: s}
	if s.Slot < 1 || s.Slot > MaxSlots {
		c.Err = fmt.Errorf("slot must be 1..%d", MaxSlots)
		return c
	}
	switch s.Mode {
	case "":
		c.Mode = ModeHide
	case ModeHide, ModeShow:
	default:
		c.Err = errors.New("mode must be hide or show")
		return c
	}
	var err error
	if strings.TrimSpace(s.OlderThan) != "" {
		if c.older, err = ParseAge(s.OlderThan); err != nil {
			c.Err = fmt.Errorf("older_than: %w", err)
			return c
		}
	}
	if strings.TrimSpace(s.YoungerThan) != "" {
		if c.younger, err = ParseAge(s.YoungerThan); err != nil {
			c.Err = fmt.Errorf("younger_than: %w", err)
			return c
		}
	}
	if s.Regex != "" {
		if c.re, err = regexp.Compile(s.Regex); err != nil {
			c.Err = fmt.Errorf("regex: %w", err)
			return c
		}
	}
	// TOML basic strings cannot carry raw control characters and Go's %q
	// would escape them as \xNN, which TOML rejects: refuse them here so
	// neither writer ever produces an unparseable file.
	for _, f := range []string{s.Name, s.Prefix, s.Suffix, s.Contains, s.Regex} {
		for _, r := range f {
			if r < 0x20 || r == 0x7f {
				c.Err = errors.New("fields must not contain control characters")
				return c
			}
		}
	}
	c.Empty = c.older == 0 && c.younger == 0 && s.Prefix == "" && s.Suffix == "" && s.Contains == "" && s.Regex == ""
	return c
}

// CompileAll places blocks by slot number (index slot-1). A later block for
// an already-filled slot is skipped (the first wins, so a hand-edit never
// silently flips a rule) and a block whose slot is out of range has no index
// to land on; both are reported in warnings so Settings can show them.
// Unfilled slots are Empty.
func CompileAll(slots []Slot) (out [MaxSlots]Compiled, warnings []string) {
	var filled [MaxSlots]bool
	for i := range out {
		out[i] = Compiled{Slot: Slot{Slot: i + 1, Mode: ModeHide}, Empty: true}
	}
	for _, s := range slots {
		if s.Slot < 1 || s.Slot > MaxSlots {
			warnings = append(warnings, fmt.Sprintf("block with slot %d ignored: slot must be 1..%d", s.Slot, MaxSlots))
			continue
		}
		if filled[s.Slot-1] {
			warnings = append(warnings, fmt.Sprintf("duplicate block for slot %d ignored (first wins)", s.Slot))
			continue
		}
		out[s.Slot-1] = Compile(s)
		filled[s.Slot-1] = true
	}
	return out, warnings
}

// Matches reports whether a branch named name (the branch PART for a remote
// branch: "feat/x", not "origin/feat/x") whose tip was committed at unixTime
// (0 = unknown) satisfies every set clause. An unknown time fails any age
// clause; an empty or inert rule matches nothing.
func (c Compiled) Matches(name string, unixTime int64, now time.Time) bool {
	if !c.Usable() {
		return false
	}
	if c.older > 0 || c.younger > 0 {
		if unixTime == 0 {
			return false
		}
		age := now.Sub(time.Unix(unixTime, 0))
		if c.older > 0 && age <= c.older {
			return false
		}
		if c.younger > 0 && age >= c.younger {
			return false
		}
	}
	if c.Prefix != "" && !strings.HasPrefix(name, c.Prefix) {
		return false
	}
	if c.Suffix != "" && !strings.HasSuffix(name, c.Suffix) {
		return false
	}
	if c.Contains != "" && !strings.Contains(name, c.Contains) {
		return false
	}
	if c.re != nil && !c.re.MatchString(name) {
		return false
	}
	return true
}

// Row is what Apply evaluates: the tested name and the tip time.
type Row struct {
	Name     string
	UnixTime int64
}

// Verdict is one row's fate under an active slot. Exempt is set when the
// rule WOULD have hidden the row but it is protected (HEAD, checked out in a
// worktree, the upstream row) — the frontends dim-mark it.
type Verdict struct {
	Hidden, Exempt bool
}

// Apply evaluates every row under c. exempt may be nil or shorter than rows
// (missing entries are false). hidden counts rows with Hidden set.
func Apply(c Compiled, rows []Row, exempt []bool, now time.Time) (v []Verdict, hidden int) {
	v = make([]Verdict, len(rows))
	if !c.Usable() {
		return v, 0
	}
	for i, r := range rows {
		m := c.Matches(r.Name, r.UnixTime, now)
		wouldHide := (c.Mode == ModeHide && m) || (c.Mode == ModeShow && !m)
		if !wouldHide {
			continue
		}
		if i < len(exempt) && exempt[i] {
			v[i].Exempt = true
			continue
		}
		v[i].Hidden = true
		hidden++
	}
	return v, hidden
}
