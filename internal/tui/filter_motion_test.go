package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestFilterMotionContract pins the shared in-filter navigation contract: arrows
// and pages move (returning true), everything else — crucially printable runes
// like j/k/z — falls through (returns false, no move) so it stays query text.
func TestFilterMotionContract(t *testing.T) {
	t.Parallel()
	const page = 12
	cases := []struct {
		name    string
		msg     tea.KeyMsg
		want    int  // expected delta passed to move (0 = not called)
		handled bool // expected return
	}{
		{"up", tea.KeyMsg{Type: tea.KeyUp}, -1, true},
		{"down", tea.KeyMsg{Type: tea.KeyDown}, 1, true},
		{"pgup", tea.KeyMsg{Type: tea.KeyPgUp}, -page, true},
		{"pgdown", tea.KeyMsg{Type: tea.KeyPgDown}, page, true},
		{"rune-j", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, 0, false},
		{"rune-k", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}, 0, false},
		{"rune-z", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")}, 0, false},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, 0, false},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, 0, false},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, called := 0, false
			handled := filterMotion(tc.msg, func(d int) { got, called = d, true }, page)
			if handled != tc.handled {
				t.Fatalf("handled=%v want %v", handled, tc.handled)
			}
			if tc.want == 0 && called {
				t.Fatalf("move should not be called for %s", tc.name)
			}
			if tc.want != 0 && got != tc.want {
				t.Fatalf("move delta=%d want %d", got, tc.want)
			}
		})
	}
}
