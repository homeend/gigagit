package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// English is the key, so with no language set every func must return the
// exact English sentence — byte-identical rendering is stage 1's contract.
func TestOpDisplayNameEnglishPassthrough(t *testing.T) {
	for _, op := range []string{"merge", "rebase", "cherry-pick", "revert"} {
		if got := opDisplayName(op); got != op {
			t.Fatalf("opDisplayName(%q) = %q, want passthrough", op, got)
		}
	}
	if got := opDisplayName("weird"); got != "weird" {
		t.Fatalf("unknown op must pass through, got %q", got)
	}
}

func TestDescribeConflictMatchesDomainEnglish(t *testing.T) {
	cases := []domain.ConflictState{
		{Op: "merge", Source: "a", Target: "b"},
		{Op: "rebase", Source: "a", Target: "b"},
		{Op: "cherry-pick", Source: "abc123"},
		{Op: "revert", Source: "abc123"},
		{},
	}
	for _, c := range cases {
		if got, want := describeConflict(c), c.Describe(); got != want {
			t.Fatalf("describeConflict(%+v) = %q, want %q (domain English)", c, got, want)
		}
	}
}

func TestConflictNoticePluralSelection(t *testing.T) {
	cases := []struct {
		n    int
		src  string
		want string
	}{
		{1, "", "⚠ 1 conflict — press [x] to resolve"},
		{1, "merging a into b", "⚠ 1 conflict merging a into b — press [x] to resolve"},
		{3, "", "⚠ 3 conflicts — press [x] to resolve"},
		{3, "merging a into b", "⚠ 3 conflicts merging a into b — press [x] to resolve"},
	}
	for _, c := range cases {
		if got := conflictNotice(c.n, c.src); got != c.want {
			t.Fatalf("conflictNotice(%d, %q) = %q, want %q", c.n, c.src, got, c.want)
		}
	}
}

func TestPausedNotice(t *testing.T) {
	if got := pausedNotice("merge", ""); got != "⏸ merge paused — press [x] to continue or abort" {
		t.Fatalf("got %q", got)
	}
	if got := pausedNotice("merge", "merging a into b"); !strings.Contains(got, "(merging a into b)") {
		t.Fatalf("source must appear parenthesised, got %q", got)
	}
}
