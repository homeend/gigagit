package model

import (
	"errors"
	"testing"
)

func TestLineFingerprint(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ line, want string }{
		{"alpha", "5d8b6dab"},
		{"  alpha\t", "5d8b6dab"}, // indentation and trailing space are not content
		{"alpha\r", "5d8b6dab"},   // a CRLF file's line
		{"beta", "af81e4c7"},
		{"x := 1", "fb8130f3"}, // inner whitespace IS content
		{"", ""},
		{" \t ", ""}, // a blank line has no fingerprint
	} {
		if got := LineFingerprint(c.line); got != c.want {
			t.Errorf("LineFingerprint(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

func TestLinkFingerprintRoundTrips(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit/a/b.go:33~5d8b6dab",
		"gg://gigagit/a/b.go:old:18~af81e4c7",
		"gg://gigagit/a/b.go@staged:7~5d8b6dab",
		"gg://gigagit/a/b.go@staged:old:7~5d8b6dab",
		"gg://gigagit/a/b.go:3~5d8b6dab?view=content",
		"gg:///home/u/repo/a.go:3~5d8b6dab",
		"gg:///C:/src/repo/a.go:3~5d8b6dab",
		"gg://gigagit/dir~x/b~c.go:3~5d8b6dab", // '~' in a path is still a path
		"gg://gigagit/dir~x/b~c.go:3",
		"gg://gigagit/dir~x/b~c.go",
	} {
		l, err := ParseLink(s)
		if err != nil {
			t.Errorf("ParseLink(%q) = %v", s, err)
			continue
		}
		if got := l.String(); got != s {
			t.Errorf("String(Parse(%q)) = %q", s, got)
		}
	}
	l, _ := ParseLink("gg://gigagit/a/b.go:old:18~af81e4c7")
	if l.Line != 18 || l.Side != NoteSideOld || l.Fingerprint != "af81e4c7" || l.Path != "a/b.go" {
		t.Errorf("parsed = %+v", l)
	}
	if l, _ := ParseLink("gg:///C:/src/repo/a.go:3~5d8b6dab"); l.Line != 3 || l.Fingerprint != "5d8b6dab" || l.Repo.Abs != "C:/src/repo/a.go" {
		t.Errorf("local form parsed = %+v", l)
	}
}

func TestLinkFingerprintRefusals(t *testing.T) {
	t.Parallel()
	sha := "0123456789abcdef0123456789abcdef01234567"
	for _, s := range []string{
		"gg://r/a.go:3~5D8B6DAB",             // upper case
		"gg://r/a.go:3~5d8b6da",              // 7 chars
		"gg://r/a.go:3~5d8b6dabc",            // 9 chars
		"gg://r/a.go:3~",                     // empty
		"gg://r/a.go:3~zzzzzzzz",             // not hex
		"gg://r/a.go@" + sha + ":3~5d8b6dab", // a commit is already fixed
		"gg://r/a.go@" + sha + ".." + sha + ":3~5d8b6dab",
		"gg://r/a.go@main...feat/x:3~5d8b6dab",
		"gg://r/a.go@ref:main:3~5d8b6dab",
	} {
		if _, err := ParseLink(s); !errors.Is(err, ErrLink) {
			t.Errorf("ParseLink(%q) = %v, want ErrLink", s, err)
		}
	}
	// String never writes a fingerprint without a line.
	l := Link{Repo: LinkRepo{Name: "r"}, Path: "a.go", Fingerprint: "5d8b6dab"}
	if got := l.String(); got != "gg://r/a.go" {
		t.Errorf("String = %q", got)
	}
}
