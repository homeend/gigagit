package domain

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestConsoleKeyEventMapsNamesAndModifiers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   ConsoleKey
		code rune
		mod  uv.KeyMod
	}{
		{ConsoleKey{K: "enter"}, uv.KeyEnter, 0},
		{ConsoleKey{K: "tab", Mod: ModShift}, uv.KeyTab, uv.ModShift},
		{ConsoleKey{K: "esc"}, uv.KeyEscape, 0},
		{ConsoleKey{K: "backspace"}, uv.KeyBackspace, 0},
		{ConsoleKey{K: "up", Mod: ModCtrl}, uv.KeyUp, uv.ModCtrl},
		{ConsoleKey{K: "f5"}, uv.KeyF5, 0},
		{ConsoleKey{K: "char", Mod: ModCtrl, Text: "c"}, 'c', uv.ModCtrl},
		{ConsoleKey{K: "char", Mod: ModAlt, Text: "x"}, 'x', uv.ModAlt},
	}
	for _, c := range cases {
		in, err := ConsoleKeyEvent(c.in)
		if err != nil || !in.IsKey {
			t.Fatalf("%+v: err=%v in=%+v", c.in, err, in)
		}
		kp, ok := in.Key.(uv.KeyPressEvent) // SessionKey is the uv.KeyEvent interface
		if !ok || kp.Code != c.code || kp.Mod != c.mod {
			t.Fatalf("%+v → %#v, want code %q mod %v", c.in, in.Key, c.code, c.mod)
		}
	}
}

func TestConsoleKeyEventPlainTextIsSentAsText(t *testing.T) {
	t.Parallel()
	for _, txt := range []string{"a", "你好", "hello"} { // an IME commit or batched runes stay one string
		in, err := ConsoleKeyEvent(ConsoleKey{K: "char", Text: txt})
		if err != nil || in.IsKey || in.Text != txt {
			t.Fatalf("%q → %+v err=%v", txt, in, err)
		}
	}
}

func TestConsoleKeyEventRefusesUnknownAndEmpty(t *testing.T) {
	t.Parallel()
	for _, k := range []ConsoleKey{{K: "bogus"}, {K: "char"}, {K: "char", Mod: ModCtrl, Text: "ab"}, {K: "enter", Mod: 99}} {
		if _, err := ConsoleKeyEvent(k); err == nil {
			t.Fatalf("%+v accepted", k)
		}
	}
}

func TestClampConsoleSize(t *testing.T) {
	t.Parallel()
	for _, c := range [][4]int{{0, 0, 20, 5}, {80, 24, 80, 24}, {9999, 9999, 500, 300}, {-3, 40, 20, 40}} {
		if w, h := ClampConsoleSize(c[0], c[1]); w != c[2] || h != c[3] {
			t.Fatalf("%dx%d → %dx%d, want %dx%d", c[0], c[1], w, h, c[2], c[3])
		}
	}
}
