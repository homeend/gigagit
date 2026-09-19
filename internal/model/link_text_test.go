package model

import "testing"

// A Link round-trips through TOML text as the link TEXT, not an exploded
// struct. The fixture is a THREE-DOT preview link on purpose: it is the shape
// the previews conversion writes, and it is the one shape whose halves are
// asymmetric, so a marshaller that dropped or swapped a half would show here.
func TestLinkMarshalTextRoundTrip(t *testing.T) {
	t.Parallel()
	const s = "gg://gigagit@main...feat/login"
	l, err := ParseLink(s)
	if err != nil {
		t.Fatalf("ParseLink(%q): %v", s, err)
	}
	b, err := l.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if string(b) != s {
		t.Fatalf("MarshalText = %q, want %q", b, s)
	}
	var back Link
	if err := back.UnmarshalText(b); err != nil {
		t.Fatalf("UnmarshalText(%q): %v", b, err)
	}
	if back.String() != s {
		t.Fatalf("round trip = %q, want %q", back.String(), s)
	}
	if back.Target.Preview == nil {
		t.Fatal("round trip lost the preview target")
	}
	if got := [2]string{back.Target.Preview.Target, back.Target.Preview.Source}; got != [2]string{"main", "feat/login"} {
		t.Fatalf("round trip halves = %v, want [main feat/login]", got)
	}
}

// UnmarshalText refuses a malformed link rather than leaving a zero Link
// behind: a corrupt stored row must fail loudly, never read as "the working
// tree of some repo".
func TestLinkUnmarshalTextRefusesGarbage(t *testing.T) {
	t.Parallel()
	var l Link
	if err := l.UnmarshalText([]byte("not-a-link")); err == nil {
		t.Fatal("UnmarshalText accepted a non-link")
	}
}
