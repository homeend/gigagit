package tui

import (
	"fmt"
	"testing"

	"github.com/homeend/gigagit/internal/theme"
)

func TestAttentionStyleResolvesEveryTone(t *testing.T) {
	t.Parallel()
	for _, tone := range []string{"info", "warn", "error"} {
		if _, ok := attnStyle(tone); !ok {
			t.Errorf("attnStyle(%q) = not ok, want a style", tone)
		}
	}
	if _, ok := attnStyle("shout"); ok {
		t.Error("attnStyle must refuse a tone outside the allowlist")
	}
	if _, ok := attnStyle(""); ok {
		t.Error("attnStyle(\"\") must be refused — the CLI always sends a tone")
	}
}

// The three tones must be visually distinct in every built-in theme, and none
// of them may be the plain frame background (an invisible band is a bug the
// user reports as "highlight does nothing").
func TestAttentionRolesAreDistinctInEveryTheme(t *testing.T) {
	t.Parallel()
	for _, th := range []theme.Theme{theme.Terminal, theme.Dark, theme.Light} {
		th := th
		t.Run(th.Name, func(t *testing.T) {
			t.Parallel()
			s := buildStyles(th)
			// lipgloss.Color is `type Color string` with no String() method
			// (lipgloss@v1.1.0 color.go:45), so a `interface{ String() string }`
			// assertion would panic — fmt.Sprint renders any TerminalColor.
			got := []string{
				fmt.Sprint(s.attnInfo.GetBackground()),
				fmt.Sprint(s.attnWarn.GetBackground()),
				fmt.Sprint(s.attnError.GetBackground()),
			}
			seen := map[string]bool{}
			for i, g := range got {
				if g == "" {
					t.Errorf("tone %d has an empty background in %s — the band would be invisible", i, th.Name)
				}
				if seen[g] {
					t.Errorf("tone %d reuses background %q in %s", i, g, th.Name)
				}
				seen[g] = true
			}
		})
	}
}
