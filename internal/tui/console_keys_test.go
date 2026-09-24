package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
)

func TestEncodeConsoleKey(t *testing.T) {
	t.Parallel()
	press := func(code rune, mod uv.KeyMod) uv.KeyPressEvent { return uv.KeyPressEvent{Code: code, Mod: mod} }
	cases := []struct {
		name string
		in   tea.KeyMsg
		text string
		pst  string
		key  any
		drop bool
	}{
		{"batched runes → text", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("héllo")}, "héllo", "", nil, false},
		{"windows bare modifier → drop", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}}, "", "", nil, true},
		{"space → text", tea.KeyMsg{Type: tea.KeySpace}, " ", "", nil, false},
		{"paste", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\nb"), Paste: true}, "", "a\nb", nil, false},
		{"alt+b", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true}, "", "", uv.KeyPressEvent{Code: 'b', Text: "b", Mod: uv.ModAlt}, false},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "", "", press(uv.KeyEnter, 0), false},
		{"alt+enter", tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, "", "", press(uv.KeyEnter, uv.ModAlt), false},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}, "", "", press(uv.KeyEscape, 0), false},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}, "", "", press(uv.KeyTab, 0), false},
		{"shift+tab", tea.KeyMsg{Type: tea.KeyShiftTab}, "", "", press(uv.KeyTab, uv.ModShift), false},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "", "", press(uv.KeyBackspace, 0), false},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "", "", press(uv.KeyUp, 0), false},
		{"ctrl+left", tea.KeyMsg{Type: tea.KeyCtrlLeft}, "", "", press(uv.KeyLeft, uv.ModCtrl), false},
		{"shift+up", tea.KeyMsg{Type: tea.KeyShiftUp}, "", "", press(uv.KeyUp, uv.ModShift), false},
		{"pgdown", tea.KeyMsg{Type: tea.KeyPgDown}, "", "", press(uv.KeyPgDown, 0), false},
		{"delete", tea.KeyMsg{Type: tea.KeyDelete}, "", "", press(uv.KeyDelete, 0), false},
		{"f5", tea.KeyMsg{Type: tea.KeyF5}, "", "", press(uv.KeyF5, 0), false},
		{"ctrl+c", tea.KeyMsg{Type: tea.KeyCtrlC}, "", "", press('c', uv.ModCtrl), false},
		{"ctrl+o", tea.KeyMsg{Type: tea.KeyCtrlO}, "", "", press('o', uv.ModCtrl), false},
		{"ctrl+underscore", tea.KeyMsg{Type: tea.KeyCtrlUnderscore}, "", "", press('_', uv.ModCtrl), false},
	}
	for _, c := range cases {
		got := encodeConsoleKey(c.in)
		if got.text != c.text || got.paste != c.pst || got.drop != c.drop {
			t.Errorf("%s: got %+v", c.name, got)
			continue
		}
		if c.key == nil {
			if got.key != nil {
				t.Errorf("%s: unexpected key %v", c.name, got.key)
			}
			continue
		}
		if got.key != c.key {
			t.Errorf("%s: key = %#v, want %#v", c.name, got.key, c.key)
		}
	}
}

func TestJoinSurrogates(t *testing.T) {
	for _, c := range []struct {
		name     string
		high     rune
		in       []rune
		want     string
		wantHigh rune
	}{
		{"pair in one message", 0, []rune{'a', 0xD83D, 0xDC4D, 'b'}, "a👍b", 0},
		{"high carried in", 0xD83D, []rune{0xDC4D}, "👍", 0},
		{"high carried out", 0, []rune{'x', 0xD83D}, "x", 0xD83D},
		{"lone low dropped", 0, []rune{0xDC4D, 'y'}, "y", 0},
		{"high then plain drops the half", 0xD83D, []rune{'z'}, "z", 0},
		{"BMP untouched", 0, []rune("世界"), "世界", 0},
	} {
		got, high := joinSurrogates(c.high, c.in)
		if string(got) != c.want || high != c.wantHigh {
			t.Errorf("%s: got %q/%U, want %q/%U", c.name, string(got), high, c.want, c.wantHigh)
		}
	}
}
