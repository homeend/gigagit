package model

import (
	"strings"
	"testing"
)

const (
	vA = "1111111111111111111111111111111111111111"
	vB = "2222222222222222222222222222222222222222"
)

// A version link is the pair link plus ?version=<unix>-<op>; it round-trips
// and the hint splits off without touching the address.
func TestVersionHintRoundTrip(t *testing.T) {
	t.Parallel()
	s := "gg://gigagit@" + vA + ".." + vB + "?version=1727000000-rebase"
	l, err := ParseLink(s)
	if err != nil {
		t.Fatalf("ParseLink(%q): %v", s, err)
	}
	if l.Hint.Kind != "version" || l.Hint.ID != "1727000000-rebase" {
		t.Fatalf("hint = %+v, want {version 1727000000-rebase}", l.Hint)
	}
	if l.Target.Pair == nil || l.Target.Pair.A != vA || l.Target.Pair.B != vB {
		t.Fatalf("pair = %+v, want %s..%s", l.Target.Pair, vA, vB)
	}
	if got := l.String(); got != s {
		t.Errorf("String() = %q, want %q", got, s)
	}
	// The local form too (a repo with no remote).
	local := "gg:///mnt/t/repo@" + vA + ".." + vB + "?version=1727000000-pull"
	if l, err := ParseLink(local); err != nil || l.String() != local {
		t.Errorf("local form: err=%v String()=%q", err, l.String())
	}
}

// Every op token the engine records spells a hint id (digits, a dash, the
// token) — LinkHintIDOK must admit them all or a producer would refuse.
func TestEveryVersionOpTokenIsAHintID(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"merge", "rebase", "pull", "interactive-rebase", "restore", "amend", "reset", "undo-commit", "delete-branch"} {
		id := BranchVersion{Unix: 1727000000, Op: op}.ID()
		if !LinkHintIDOK(id) {
			t.Errorf("LinkHintIDOK(%q) = false", id)
		}
		if _, err := ParseLink("gg://r@" + vA + ".." + vB + "?version=" + id); err != nil {
			t.Errorf("ParseLink with id %q: %v", id, err)
		}
	}
}

// ID() is the ref's last path element — the token `gg versions` prints.
func TestBranchVersionIDIsTheRefTail(t *testing.T) {
	t.Parallel()
	v := BranchVersion{Ref: "refs/gg/versions/feat/x/1727000000-interactive-rebase", Unix: 1727000000, Op: "interactive-rebase"}
	if got := v.ID(); got != "1727000000-interactive-rebase" || !strings.HasSuffix(v.Ref, "/"+got) {
		t.Fatalf("ID() = %q, want the ref tail", got)
	}
}
