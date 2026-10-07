package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/theme"
)

func TestNoteBadgeGroupsDrawsOneBarPerGroup(t *testing.T) {
	t.Parallel()
	if got := noteBadgeGroups(3, nil); got != noteBadge(3) {
		t.Fatalf("no groups = the plain badge, got %q", got)
	}
	if got := ansi.Strip(noteBadgeGroups(4, []string{"mine", "review:r1"})); got != "  ▌▌◆ 4" {
		t.Fatalf("badge = %q", got)
	}
	if many := ansi.Strip(noteBadgeGroups(9, []string{"a", "b", "c", "d", "e"})); strings.Count(many, "▌") != 3 {
		t.Fatalf("at most 3 bars, got %q", many)
	}
}

// Sets the colour profile and theme (process-global): serial.
func TestNoteBadgeBarsWearTheirGroupsColour(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark)
	bar, _ := groupBarStyle(groupSlot("review:r1"))
	if !strings.Contains(noteBadgeGroups(4, []string{"review:r1"}), bar.Render("▌")) {
		t.Fatal("the bar wears its group's colour")
	}
}

func TestPRCountsMsgCarriesGroups(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	nm, _ := m.Update(prCountsMsg{gen: m.previewGen, counts: map[string]int{"a.txt": 2}, groups: map[string][]string{"a.txt": {"mine"}}})
	mm := nm.(Model)
	if g := mm.filesPreviewGroups["a.txt"]; len(g) != 1 || g[0] != "mine" {
		t.Fatalf("groups = %v", mm.filesPreviewGroups)
	}
}
