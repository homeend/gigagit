package model

import (
	"errors"
	"strings"
	"testing"
)

func TestParseLinkRange(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 40)
	cases := []struct {
		in        string
		line, end int
		side      NoteSide
		fp        string
	}{
		{"gg://r/a.go:3-7", 3, 7, NoteSideNew, ""},
		{"gg://r/a.go@staged:old:3-7", 3, 7, NoteSideOld, ""},
		{"gg://r/a.go:3-7~0123abcd", 3, 7, NoteSideNew, "0123abcd"},
		{"gg://r/a.go:3-3", 3, 0, NoteSideNew, ""},
		{"gg:///C:/src/r/a-b~c.go:3-7", 3, 7, NoteSideNew, ""},
		{"gg://r/a.go@" + sha + ":3-7", 3, 7, NoteSideNew, ""},
	}
	for _, c := range cases {
		l, err := ParseLink(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if l.Line != c.line || l.End != c.end || l.Side != c.side || l.Fingerprint != c.fp {
			t.Errorf("%s: got %d-%d %s %q", c.in, l.Line, l.End, l.Side, l.Fingerprint)
		}
	}
}

func TestParseLinkRangeErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"gg://r/a.go:7-3",
		"gg://r/a.go:0-3",
		"gg://r/a.go:3-",
		"gg://r/a.go:3-7~xyz",
		"gg://r/a.go@" + strings.Repeat("a", 40) + ":3-7~0123abcd",
	} {
		if _, err := ParseLink(in); !errors.Is(err, ErrLink) {
			t.Errorf("%s: %v", in, err)
		}
	}
}

// A drive colon followed by a path that merely LOOKS range-like is a path.
func TestParseLinkRangeLeavesPathsAlone(t *testing.T) {
	t.Parallel()
	l, err := ParseLink("gg:///C:/src/r/3-7x.go")
	if err != nil || l.Line != 0 || l.End != 0 {
		t.Fatalf("got %+v, %v", l, err)
	}
}

func TestLinkRangeString(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"gg://r/a.go:3-7", "gg://r/a.go@staged:old:3-7~0123abcd"} {
		l, err := ParseLink(s)
		if err != nil {
			t.Fatal(err)
		}
		if got := l.String(); got != s {
			t.Errorf("round trip: %s → %s", s, got)
		}
	}
	l := Link{Repo: LinkRepo{Name: "r"}, Path: "a.go", Target: LinkTarget{State: StateUnstaged}, Side: NoteSideNew, Line: 3, End: 3}
	if got := l.String(); got != "gg://r/a.go:3" {
		t.Errorf("End == Line prints as one line, got %s", got)
	}
}

func TestBlockFingerprint(t *testing.T) {
	t.Parallel()
	a := BlockFingerprint([]string{"  x := 1", "", "return x\r"})
	if a != BlockFingerprint([]string{"x := 1", "", "\treturn x"}) {
		t.Error("indent / CR must not matter")
	}
	if a == BlockFingerprint([]string{"x := 1", "return x"}) {
		t.Error("a dropped blank line is a change")
	}
	if BlockFingerprint([]string{" ", ""}) != "" {
		t.Error("an all-blank block has no fingerprint")
	}
	if !LinkFingerprintOK(a) {
		t.Errorf("shape: %q", a)
	}
}
