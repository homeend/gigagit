package branchfilter

import (
	"testing"
	"time"
)

var now = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) int64 { return now.Add(-d).Unix() }

func TestParseAge(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"90d", 90 * 24 * time.Hour, true},
		{"12w", 12 * 7 * 24 * time.Hour, true},
		{"6m", 6 * 30 * 24 * time.Hour, true},
		{"1y", 365 * 24 * time.Hour, true},
		{" 3d ", 3 * 24 * time.Hour, true},
		{"", 0, false},
		{"d", 0, false},
		{"-3d", 0, false},
		{"0d", 0, false},
		{"3", 0, false},
		{"3h", 0, false},
		{"3dd", 0, false},
	}
	for _, c := range cases {
		got, err := ParseAge(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("ParseAge(%q) = %v, %v; want %v ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
	// FormatAge picks the LARGEST exact unit, so a value that is an exact
	// multiple of a larger unit canonicalizes to that unit even when typed
	// in a smaller one: 90d is exactly 3 months, 14d is exactly 2 weeks.
	// A true round trip only holds when the input already used its
	// canonical (largest) unit — that's "12w", "6m", "1y", and any day
	// count that isn't itself a whole number of weeks or months.
	for _, c := range []struct{ in, want string }{
		{"90d", "3m"},
		{"12w", "12w"},
		{"6m", "6m"},
		{"1y", "1y"},
		{"14d", "2w"},
		{"10d", "10d"},
	} {
		d, _ := ParseAge(c.in)
		if got := FormatAge(d); got != c.want {
			t.Errorf("FormatAge(ParseAge(%q)) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestCompileInertReasons(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    Slot
		want string // substring of Err
	}{
		{"slot 0", Slot{Slot: 0, Prefix: "x"}, "slot must be 1..5"},
		{"slot 6", Slot{Slot: 6, Prefix: "x"}, "slot must be 1..5"},
		{"bad mode", Slot{Slot: 1, Mode: "drop", Prefix: "x"}, "mode must be hide or show"},
		{"bad age", Slot{Slot: 1, OlderThan: "soon"}, "older_than"},
		{"bad younger", Slot{Slot: 1, YoungerThan: "3h"}, "younger_than"},
		{"bad regex", Slot{Slot: 1, Regex: "("}, "regex"},
		{"control char", Slot{Slot: 1, Prefix: "a\x01b"}, "control"},
		{"control char in name", Slot{Slot: 1, Name: "x\ty", Prefix: "a"}, "control"},
	}
	for _, c := range cases {
		got := Compile(c.s)
		if got.Err == nil || !contains(got.Err.Error(), c.want) {
			t.Errorf("%s: Err = %v; want containing %q", c.name, got.Err, c.want)
		}
		if got.Usable() {
			t.Errorf("%s: inert slot reported usable", c.name)
		}
	}
}

func TestCompileEmptyAndDefaults(t *testing.T) {
	t.Parallel()
	c := Compile(Slot{Slot: 3})
	if c.Err != nil || !c.Empty || c.Usable() {
		t.Fatalf("empty slot: Err=%v Empty=%v Usable=%v", c.Err, c.Empty, c.Usable())
	}
	if c.Label() != "slot 3" {
		t.Errorf("Label = %q", c.Label())
	}
	c = Compile(Slot{Slot: 1, Name: "stale", Prefix: "feat/"})
	if c.Mode != ModeHide {
		t.Errorf("default mode = %q; want hide", c.Mode)
	}
	if c.Label() != "stale" {
		t.Errorf("Label = %q", c.Label())
	}
}

func TestMatchesClauses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    Slot
		row  string
		age  int64
		want bool
	}{
		{"prefix hit", Slot{Slot: 1, Prefix: "feat/"}, "feat/x", ago(time.Hour), true},
		{"prefix miss", Slot{Slot: 1, Prefix: "feat/"}, "fix/x", ago(time.Hour), false},
		{"prefix case-sensitive", Slot{Slot: 1, Prefix: "Feat/"}, "feat/x", ago(time.Hour), false},
		{"suffix", Slot{Slot: 1, Suffix: "-wip"}, "a-wip", ago(time.Hour), true},
		{"contains", Slot{Slot: 1, Contains: "hot"}, "x/hotfix/y", ago(time.Hour), true},
		{"regex", Slot{Slot: 1, Regex: `^release/\d+$`}, "release/12", ago(time.Hour), true},
		{"regex unanchored", Slot{Slot: 1, Regex: `\d`}, "v1", ago(time.Hour), true},
		{"older hit", Slot{Slot: 1, OlderThan: "90d"}, "x", ago(91 * 24 * time.Hour), true},
		{"older miss", Slot{Slot: 1, OlderThan: "90d"}, "x", ago(89 * 24 * time.Hour), false},
		{"younger hit", Slot{Slot: 1, YoungerThan: "7d"}, "x", ago(24 * time.Hour), true},
		{"younger miss", Slot{Slot: 1, YoungerThan: "7d"}, "x", ago(8 * 24 * time.Hour), false},
		{"unknown time fails older", Slot{Slot: 1, OlderThan: "1d"}, "x", 0, false},
		{"unknown time fails younger", Slot{Slot: 1, YoungerThan: "1y"}, "x", 0, false},
		{"AND both hold", Slot{Slot: 1, Prefix: "feat/", OlderThan: "30d"}, "feat/x", ago(40 * 24 * time.Hour), true},
		{"AND one fails", Slot{Slot: 1, Prefix: "feat/", OlderThan: "30d"}, "feat/x", ago(10 * 24 * time.Hour), false},
		{"empty matches nothing", Slot{Slot: 1}, "anything", ago(time.Hour), false},
	}
	for _, c := range cases {
		got := Compile(c.s).Matches(c.row, c.age, now)
		if got != c.want {
			t.Errorf("%s: Matches = %v; want %v", c.name, got, c.want)
		}
	}
}

func TestApplyModesAndExempt(t *testing.T) {
	t.Parallel()
	rows := []Row{{"feat/a", ago(time.Hour)}, {"main", ago(time.Hour)}, {"feat/b", ago(time.Hour)}}
	exempt := []bool{false, true, true} // main is HEAD, feat/b checked out elsewhere

	hide := Compile(Slot{Slot: 1, Mode: ModeHide, Prefix: "feat/"})
	v, n := Apply(hide, rows, exempt, now)
	want := []Verdict{{Hidden: true}, {}, {Exempt: true}}
	if n != 1 || !equalVerdicts(v, want) {
		t.Errorf("hide: v=%v n=%d; want %v n=1", v, n, want)
	}

	show := Compile(Slot{Slot: 2, Mode: ModeShow, Prefix: "feat/"})
	v, n = Apply(show, rows, exempt, now)
	want = []Verdict{{}, {Exempt: true}, {}}
	if n != 0 || !equalVerdicts(v, want) {
		t.Errorf("show: v=%v n=%d; want %v n=0", v, n, want)
	}

	// An unusable slot hides nothing and marks nothing.
	v, n = Apply(Compile(Slot{Slot: 1, Regex: "("}), rows, exempt, now)
	if n != 0 || !equalVerdicts(v, []Verdict{{}, {}, {}}) {
		t.Errorf("inert: v=%v n=%d", v, n)
	}
	// nil exempt is allowed.
	v, n = Apply(hide, rows, nil, now)
	if n != 2 || !v[2].Hidden {
		t.Errorf("nil exempt: v=%v n=%d", v, n)
	}
}

func TestCompileAll(t *testing.T) {
	t.Parallel()
	all, warnings := CompileAll([]Slot{
		{Slot: 2, Name: "a", Prefix: "a/"},
		{Slot: 2, Name: "b", Prefix: "b/"}, // duplicate: skipped + warned
		{Slot: 7, Prefix: "x"},             // out of range: skipped + warned
		{Slot: 5, Name: "e", Suffix: "e"},
	})
	if len(warnings) != 2 || !contains(warnings[0], "duplicate block for slot 2") || !contains(warnings[1], "slot 7") {
		t.Errorf("warnings = %q", warnings)
	}
	if _, w := CompileAll(nil); len(w) != 0 {
		t.Errorf("no blocks → warnings %q", w)
	}
	if all[1].Name != "a" || !all[1].Usable() {
		t.Errorf("slot 2 = %+v; want the first block", all[1].Slot)
	}
	if all[0].Err != nil || !all[0].Empty {
		t.Errorf("slot 1 should be empty: %+v", all[0])
	}
	if !all[4].Usable() {
		t.Errorf("slot 5 unusable: %v", all[4].Err)
	}
	if s := all[1].Summary(); s != "hide · prefix a/" {
		t.Errorf("Summary = %q", s)
	}
	if s := Compile(Slot{Slot: 1, Mode: ModeShow, OlderThan: "90d", Prefix: "feat/", Regex: `x$`}).Summary(); s != "show only · older than 90d, prefix feat/, regex x$" {
		t.Errorf("Summary = %q", s)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
func equalVerdicts(a, b []Verdict) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
