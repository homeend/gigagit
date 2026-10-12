package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
)

func TestMarkerFieldOverviewCell(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		id   commitIdent
		want string
	}{
		{commitIdent{}, "     "},
		{commitIdent{reviewed: true}, "  ✎  "},
		{commitIdent{overview: true}, "   ≡ "},
		{commitIdent{tip: true, reviewed: true, overview: true}, "↓ ✎≡ "},
	} {
		got := c.id.markerField()
		if got != c.want || lipgloss.Width(got) != commitMarkerW {
			t.Errorf("markerField(%+v) = %q, want %q (width %d)", c.id, got, c.want, commitMarkerW)
		}
	}
}

func TestCommitHasOverviewFromAnyBranch(t *testing.T) {
	t.Parallel()
	m := Model{}
	m.noteCounts.Overviews = []domain.OverviewHead{{ID: "o1", Commit: "abc123", Branch: "feat"}}
	m.status.Branch = "main" // viewing another branch: still marked (R5)
	if !m.commitHasOverview("abc123") || m.commitHasOverview("def456") {
		t.Fatal("commitHasOverview")
	}
}
