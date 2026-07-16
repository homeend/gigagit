package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

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

func TestSourceDisplayNameEnglishPassthrough(t *testing.T) {
	for s := sourceKey(0); s < srcCount; s++ {
		if got := sourceDisplayName(s); got != sourceNames[s] {
			t.Fatalf("sourceDisplayName(%d) = %q, want %q", s, got, sourceNames[s])
		}
	}
}

func TestOptionDisplayNameEnglishPassthrough(t *testing.T) {
	// English is the key: with no language set, every value maps to itself.
	for _, v := range []string{
		"Apply patch", "Cancel", "Cherry-pick", "Create branch…",
		"Create worktree…", "Delete", "Detached", "Discard", "No",
		"Push branch + tags", "Push branch only", "Remove",
		"Reorder & squash", "Yes", "abort", "cancel",
		"check out as different name…", "checkout-and-resolve", "commits",
		"delete", "force", "force-delete", "force-with-lease",
		"go to worktree", "hard", "keep", "keep-conflicts", "merge",
		"mixed", "no", "proceed", "rebase", "reset", "run", "skip", "soft",
		"unlock-and-remove", "working-tree", "yes",
	} {
		if got := optionDisplayName(v); got != v {
			t.Fatalf("optionDisplayName(%q) = %q, want passthrough", v, got)
		}
	}
	if got := optionDisplayName("feature/dynamic-branch-name"); got != "feature/dynamic-branch-name" {
		t.Fatalf("unknown value must pass through, got %q", got)
	}
}

// padCell now lives in i18n_display.go (moved from settings_popup.go) so the
// identity popup's label columns can share it. This pins the alignment
// property both call sites rely on: a wider CJK label still pads to the same
// display width as a narrower ASCII one.
func TestIdentityLabelPadCellAlignsCJK(t *testing.T) {
	ascii := "  " + padCell("Name", 9) + " v"
	cjk := "  " + padCell("名前", 9) + " v"
	if lipgloss.Width(ascii) != lipgloss.Width(cjk) {
		t.Fatalf("padded label columns misalign: %d vs %d", lipgloss.Width(ascii), lipgloss.Width(cjk))
	}
}
