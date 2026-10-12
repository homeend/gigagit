package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/theme"
)

func TestMetaClassPaints(t *testing.T) {
	t.Parallel()
	s := buildStyles(theme.Dark)
	base := lipgloss.NewStyle()
	if metaClass("crimson", notemeta.StyleText) != 0 {
		t.Fatal("unknown colour must be class 0")
	}
	txt := s.metaStyle(base, metaClass("red", notemeta.StyleText))
	if txt.GetForeground() != lipgloss.Color(theme.Dark.MetaRed) {
		t.Fatalf("text fg = %v", txt.GetForeground())
	}
	fill := s.metaStyle(base, metaClass("green", notemeta.StyleFilled))
	if fill.GetBackground() != lipgloss.Color(theme.Dark.MetaGreen) || fill.GetForeground() != lipgloss.Color(theme.Dark.Bg) {
		t.Fatalf("filled = bg %v fg %v", fill.GetBackground(), fill.GetForeground())
	}
	// syntaxStyle routes the meta classes: one mask paints everywhere.
	if s.syntaxStyle(base, metaClass("red", notemeta.StyleText)).GetForeground() != lipgloss.Color(theme.Dark.MetaRed) {
		t.Fatal("syntaxStyle does not reach metaStyle")
	}
	term := buildStyles(theme.Terminal)
	if term.metaStyle(base, metaClass("orange", notemeta.StyleText)).GetForeground() == term.metaStyle(base, metaClass("yellow", notemeta.StyleText)).GetForeground() {
		t.Fatal("Terminal orange and yellow must differ")
	}
}
