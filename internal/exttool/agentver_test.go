package exttool

import (
	"regexp"
	"testing"
)

func TestParseVersion(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]Version{"2.1": {2, 1, 0}, "2.1.7": {2, 1, 7}, "10.0.1": {10, 0, 1}} {
		got, ok := ParseVersion(in)
		if !ok || got != want {
			t.Errorf("ParseVersion(%q) = %v,%v want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "2", "x.y", "2..1"} {
		if _, ok := ParseVersion(bad); ok {
			t.Errorf("ParseVersion(%q) ok, want failure", bad)
		}
	}
}

func TestRangeContains(t *testing.T) {
	t.Parallel()
	r, err := ParseRange(">=1.8 <2.1")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[Version]bool{{1, 7, 9}: false, {1, 8, 0}: true, {2, 0, 99}: true, {2, 1, 0}: false}
	for v, want := range cases {
		if got := r.Contains(v); got != want {
			t.Errorf("%v in %q = %v, want %v", v, ">=1.8 <2.1", got, want)
		}
	}
	anyV, _ := ParseRange("")
	if !anyV.Contains(Version{0, 0, 1}) {
		t.Error(`"" must contain every version`)
	}
	for _, bad := range []string{">2", "<=3", ">=a", ">=2 >=3"} {
		if _, err := ParseRange(bad); err == nil {
			t.Errorf("ParseRange(%q) accepted", bad)
		}
	}
}

func TestRangeOverlaps(t *testing.T) {
	t.Parallel()
	mk := func(s string) Range { r, _ := ParseRange(s); return r }
	if mk(">=1.8 <2.1").Overlaps(mk(">=2.1")) {
		t.Error("adjacent half-open ranges must not overlap")
	}
	if !mk(">=1.8 <2.2").Overlaps(mk(">=2.1")) {
		t.Error("[1.8,2.2) and [2.1,∞) overlap")
	}
	if !mk("").Overlaps(mk(">=9.0")) {
		t.Error(`"" overlaps everything`)
	}
}

func TestExtractVersion(t *testing.T) {
	t.Parallel()
	v, ok := ExtractVersion([]byte("2.1.7 (Claude Code)\n"), nil)
	if !ok || v != (Version{2, 1, 7}) {
		t.Fatalf("default re: %v %v", v, ok)
	}
	re := regexp.MustCompile(`junie ([0-9.]+)`)
	v, ok = ExtractVersion([]byte("build 99.1\njunie 4.2\n"), re)
	if !ok || v != (Version{4, 2, 0}) {
		t.Fatalf("custom re: %v %v", v, ok)
	}
	if _, ok := ExtractVersion([]byte("no digits"), nil); ok {
		t.Fatal("garbage parsed")
	}
}
