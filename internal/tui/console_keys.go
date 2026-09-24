package tui

import (
	"strings"
	"unicode/utf16"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/homeend/gigagit/internal/domain"
)

// consoleInput is what one tea.KeyMsg sends to a session: exactly one of
// text, paste, key is set; drop = send nothing.
type consoleInput struct {
	text  string
	paste string
	key   domain.SessionKey
	drop  bool
}

// consoleSpecial maps Bubble Tea's non-rune key types to the key code and
// modifiers the session's emulator encodes (it honours the child's terminal
// modes, e.g. application cursor keys).
// Aliases share a KeyType value in Bubble Tea v1 and appear once: KeyEnter
// is ctrl+m, KeyTab ctrl+i, KeyEsc ctrl+[, KeyBackspace ctrl+? (DEL).
var consoleSpecial = map[tea.KeyType]uv.KeyPressEvent{
	tea.KeyEnter: {Code: uv.KeyEnter}, tea.KeyTab: {Code: uv.KeyTab},
	tea.KeyShiftTab: {Code: uv.KeyTab, Mod: uv.ModShift},
	tea.KeyEsc:      {Code: uv.KeyEscape}, tea.KeyBackspace: {Code: uv.KeyBackspace},
	tea.KeyDelete: {Code: uv.KeyDelete}, tea.KeyInsert: {Code: uv.KeyInsert},
	tea.KeyUp: {Code: uv.KeyUp}, tea.KeyDown: {Code: uv.KeyDown},
	tea.KeyLeft: {Code: uv.KeyLeft}, tea.KeyRight: {Code: uv.KeyRight},
	tea.KeyHome: {Code: uv.KeyHome}, tea.KeyEnd: {Code: uv.KeyEnd},
	tea.KeyPgUp: {Code: uv.KeyPgUp}, tea.KeyPgDown: {Code: uv.KeyPgDown},
	tea.KeyShiftUp: {Code: uv.KeyUp, Mod: uv.ModShift}, tea.KeyShiftDown: {Code: uv.KeyDown, Mod: uv.ModShift},
	tea.KeyShiftLeft: {Code: uv.KeyLeft, Mod: uv.ModShift}, tea.KeyShiftRight: {Code: uv.KeyRight, Mod: uv.ModShift},
	tea.KeyShiftHome: {Code: uv.KeyHome, Mod: uv.ModShift}, tea.KeyShiftEnd: {Code: uv.KeyEnd, Mod: uv.ModShift},
	tea.KeyCtrlUp: {Code: uv.KeyUp, Mod: uv.ModCtrl}, tea.KeyCtrlDown: {Code: uv.KeyDown, Mod: uv.ModCtrl},
	tea.KeyCtrlLeft: {Code: uv.KeyLeft, Mod: uv.ModCtrl}, tea.KeyCtrlRight: {Code: uv.KeyRight, Mod: uv.ModCtrl},
	tea.KeyCtrlHome: {Code: uv.KeyHome, Mod: uv.ModCtrl}, tea.KeyCtrlEnd: {Code: uv.KeyEnd, Mod: uv.ModCtrl},
	tea.KeyCtrlPgUp: {Code: uv.KeyPgUp, Mod: uv.ModCtrl}, tea.KeyCtrlPgDown: {Code: uv.KeyPgDown, Mod: uv.ModCtrl},
	tea.KeyCtrlShiftUp: {Code: uv.KeyUp, Mod: uv.ModCtrl | uv.ModShift}, tea.KeyCtrlShiftDown: {Code: uv.KeyDown, Mod: uv.ModCtrl | uv.ModShift},
	tea.KeyCtrlShiftLeft: {Code: uv.KeyLeft, Mod: uv.ModCtrl | uv.ModShift}, tea.KeyCtrlShiftRight: {Code: uv.KeyRight, Mod: uv.ModCtrl | uv.ModShift},
	tea.KeyCtrlAt: {Code: '@', Mod: uv.ModCtrl}, tea.KeyCtrlBackslash: {Code: '\\', Mod: uv.ModCtrl},
	tea.KeyCtrlCloseBracket: {Code: ']', Mod: uv.ModCtrl}, tea.KeyCtrlCaret: {Code: '^', Mod: uv.ModCtrl},
	tea.KeyCtrlUnderscore: {Code: '_', Mod: uv.ModCtrl},
	tea.KeyF1:             {Code: uv.KeyF1}, tea.KeyF2: {Code: uv.KeyF2}, tea.KeyF3: {Code: uv.KeyF3}, tea.KeyF4: {Code: uv.KeyF4},
	tea.KeyF5: {Code: uv.KeyF5}, tea.KeyF6: {Code: uv.KeyF6}, tea.KeyF7: {Code: uv.KeyF7}, tea.KeyF8: {Code: uv.KeyF8},
	tea.KeyF9: {Code: uv.KeyF9}, tea.KeyF10: {Code: uv.KeyF10}, tea.KeyF11: {Code: uv.KeyF11}, tea.KeyF12: {Code: uv.KeyF12},
}

// encodeConsoleKey turns one Bubble Tea key into session input.
func encodeConsoleKey(k tea.KeyMsg) consoleInput {
	if k.Paste {
		return consoleInput{paste: string(k.Runes)}
	}
	switch k.Type {
	case tea.KeyRunes:
		s := string(k.Runes)
		if strings.Trim(s, "\x00") == "" {
			// Windows: a bare Ctrl/Alt/Win key-down arrives as KeyRunes{0}
			// (Bubble Tea's console reader filters only Shift).
			return consoleInput{drop: true}
		}
		if k.Alt && len(k.Runes) == 1 {
			return consoleInput{key: uv.KeyPressEvent{Code: k.Runes[0], Text: s, Mod: uv.ModAlt}}
		}
		return consoleInput{text: s} // batched fast typing arrives as one message
	case tea.KeySpace:
		return consoleInput{text: " "}
	}
	if ev, ok := consoleSpecial[k.Type]; ok {
		if k.Alt {
			ev.Mod |= uv.ModAlt
		}
		return consoleInput{key: ev}
	}
	if k.Type >= tea.KeyCtrlA && k.Type <= tea.KeyCtrlZ {
		ev := uv.KeyPressEvent{Code: rune('a' + int(k.Type-tea.KeyCtrlA)), Mod: uv.ModCtrl}
		if k.Alt {
			ev.Mod |= uv.ModAlt
		}
		return consoleInput{key: ev}
	}
	return consoleInput{drop: true}
}

// joinSurrogates rebuilds characters outside the BMP from UTF-16 halves:
// Bubble Tea's Windows console reader hands over one UTF-16 unit per rune,
// so an emoji arrives as a high then a low surrogate, possibly in separate
// messages. high is a half carried over from the previous message; the
// returned rune is the half to carry into the next. A half with no partner
// is dropped rather than sent as U+FFFD.
func joinSurrogates(high rune, rs []rune) ([]rune, rune) {
	out := make([]rune, 0, len(rs))
	for _, r := range rs {
		switch {
		case r >= 0xD800 && r < 0xDC00:
			high = r
		case r >= 0xDC00 && r < 0xE000:
			if high != 0 {
				out = append(out, utf16.DecodeRune(high, r))
			}
			high = 0
		default:
			high = 0
			out = append(out, r)
		}
	}
	return out, high
}
