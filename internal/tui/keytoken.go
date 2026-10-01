package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// namedKeys is keyToken's named vocabulary, inverted.
var namedKeys = map[string]tea.KeyType{
	"enter": tea.KeyEnter, "esc": tea.KeyEsc, "space": tea.KeySpace, "tab": tea.KeyTab,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"bspace": tea.KeyBackspace, "delete": tea.KeyDelete, "home": tea.KeyHome, "end": tea.KeyEnd,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
}

// ctrlKeys maps "ctrl+x" (tea.KeyMsg.String) to its KeyType, built once
// from Bubble Tea's own names so it can never disagree with keyToken.
var ctrlKeys = func() map[string]tea.KeyType {
	out := map[string]tea.KeyType{}
	for kt := tea.KeyType(-200); kt < 200; kt++ {
		if s := (tea.KeyMsg{Type: kt}).String(); strings.HasPrefix(s, "ctrl+") {
			if _, dup := out[s]; !dup {
				out[s] = kt
			}
		}
	}
	return out
}()

// keyMsgFor is keyToken's inverse: one keyscript token → one key. The
// headless driver (headless.go) presses keys through it, so what
// `gg --record` writes is exactly what a golden-screen scenario accepts.
func keyMsgFor(tok string) (tea.KeyMsg, error) {
	if strings.HasPrefix(tok, "M-") && len(tok) > 2 {
		k, err := keyMsgFor(tok[2:])
		if err != nil {
			return k, err
		}
		k.Alt = true
		return k, nil
	}
	if kt, ok := namedKeys[tok]; ok {
		k := tea.KeyMsg{Type: kt}
		if kt == tea.KeySpace {
			k.Runes = []rune{' '}
		}
		return k, nil
	}
	if strings.HasPrefix(tok, "C-") && len(tok) > 2 {
		if kt, ok := ctrlKeys["ctrl+"+tok[2:]]; ok {
			return tea.KeyMsg{Type: kt}, nil
		}
		return tea.KeyMsg{}, fmt.Errorf("unknown ctrl chord %q", tok)
	}
	if r := []rune(tok); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}, nil
	}
	return tea.KeyMsg{}, fmt.Errorf("not a single keyscript token: %q", tok)
}

// splitLiteral turns a step's token into presses: a named key or chord is
// one press, a multi-rune literal is one press per rune (as the recorder
// writes it). A C-/M- token is always a chord, so a mistyped one fails in
// keyMsgFor instead of being pressed as letters.
func splitLiteral(tok string) []string {
	if _, err := keyMsgFor(tok); err == nil || isChord(tok) {
		return []string{tok}
	}
	var out []string
	for _, r := range tok {
		out = append(out, string(r))
	}
	return out
}

func isChord(tok string) bool {
	return len(tok) > 2 && (strings.HasPrefix(tok, "C-") || strings.HasPrefix(tok, "M-"))
}

// isDiagnostic reports keyToken's "<...>" fallback for a key with no name —
// not the "<" key itself.
func isDiagnostic(tok string) bool {
	return len(tok) > 2 && strings.HasPrefix(tok, "<") && strings.HasSuffix(tok, ">")
}
