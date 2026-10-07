package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/theme"
)

func TestAnchorBandStylesFollowTheTheme(t *testing.T) {
	t.Parallel()
	s := buildStyles(theme.Dark)
	if _, none := s.anchorBand.GetBackground().(lipgloss.NoColor); none {
		t.Fatal("dark theme: the other anchors' band has no background")
	}
	if _, none := s.anchorBandCur.GetBackground().(lipgloss.NoColor); none {
		t.Fatal("dark theme: the current anchor's band has no background")
	}
	if s.anchorBand.GetBackground() == s.anchorBandCur.GetBackground() {
		t.Fatal("dark theme: current and other bands share a colour")
	}
	s = buildStyles(theme.Terminal)
	if _, none := s.anchorBand.GetBackground().(lipgloss.NoColor); !none {
		t.Fatal("terminal theme: an anchor band paints a background")
	}
}
