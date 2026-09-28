package agentsession

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestEncodeModifiedKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		key  uv.KeyPressEvent
		want string
	}{
		{"ctrl+left", uv.KeyPressEvent{Code: uv.KeyLeft, Mod: uv.ModCtrl}, "\x1b[1;5D"},
		{"ctrl+right", uv.KeyPressEvent{Code: uv.KeyRight, Mod: uv.ModCtrl}, "\x1b[1;5C"},
		{"shift+up", uv.KeyPressEvent{Code: uv.KeyUp, Mod: uv.ModShift}, "\x1b[1;2A"},
		{"alt+down", uv.KeyPressEvent{Code: uv.KeyDown, Mod: uv.ModAlt}, "\x1b[1;3B"},
		{"ctrl+shift+left", uv.KeyPressEvent{Code: uv.KeyLeft, Mod: uv.ModCtrl | uv.ModShift}, "\x1b[1;6D"},
		{"ctrl+alt+right", uv.KeyPressEvent{Code: uv.KeyRight, Mod: uv.ModCtrl | uv.ModAlt}, "\x1b[1;7C"},
		{"ctrl+home", uv.KeyPressEvent{Code: uv.KeyHome, Mod: uv.ModCtrl}, "\x1b[1;5H"},
		{"shift+end", uv.KeyPressEvent{Code: uv.KeyEnd, Mod: uv.ModShift}, "\x1b[1;2F"},
		{"ctrl+pgup", uv.KeyPressEvent{Code: uv.KeyPgUp, Mod: uv.ModCtrl}, "\x1b[5;5~"},
		{"ctrl+pgdown", uv.KeyPressEvent{Code: uv.KeyPgDown, Mod: uv.ModCtrl}, "\x1b[6;5~"},
		{"shift+delete", uv.KeyPressEvent{Code: uv.KeyDelete, Mod: uv.ModShift}, "\x1b[3;2~"},
		{"alt+insert", uv.KeyPressEvent{Code: uv.KeyInsert, Mod: uv.ModAlt}, "\x1b[2;3~"},
		{"shift+f1", uv.KeyPressEvent{Code: uv.KeyF1, Mod: uv.ModShift}, "\x1b[1;2P"},
		{"ctrl+f5", uv.KeyPressEvent{Code: uv.KeyF5, Mod: uv.ModCtrl}, "\x1b[15;5~"},
		{"alt+f12", uv.KeyPressEvent{Code: uv.KeyF12, Mod: uv.ModAlt}, "\x1b[24;3~"},
	}
	for _, c := range cases {
		got, ok := encodeModifiedKey(c.key)
		if !ok || got != c.want {
			t.Errorf("%s: got %q ok=%v, want %q", c.name, got, ok, c.want)
		}
	}
}

// Keys the emulator already encodes correctly stay on its path: unmodified
// specials (it honours application cursor mode), ctrl+letters, shift+tab,
// alt+rune, and plain text.
func TestEncodeModifiedKeyLeavesTheRestToTheEmulator(t *testing.T) {
	t.Parallel()
	for name, k := range map[string]uv.KeyPressEvent{
		"left":      {Code: uv.KeyLeft},
		"enter":     {Code: uv.KeyEnter},
		"ctrl+a":    {Code: 'a', Mod: uv.ModCtrl},
		"shift+tab": {Code: uv.KeyTab, Mod: uv.ModShift},
		"alt+b":     {Code: 'b', Text: "b", Mod: uv.ModAlt},
		"x":         {Code: 'x', Text: "x"},
		"f1":        {Code: uv.KeyF1},
	} {
		if got, ok := encodeModifiedKey(k); ok {
			t.Errorf("%s: encoded as %q, want the emulator's own encoding", name, got)
		}
	}
}

// The bytes the child sees: a raw-mode cat -v prints control sequences as
// ^[[...]. Before the encoder, the emulator wrote NOTHING for ctrl+left.
func TestSendKeyModifiedArrowReachesChild(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, "stty raw -echo; cat -v")
	s.SendKey(uv.KeyPressEvent{Code: uv.KeyLeft, Mod: uv.ModCtrl})
	s.SendKey(uv.KeyPressEvent{Code: uv.KeyLeft}) // the plain arrow still goes through the emulator
	s.SendKey(uv.KeyPressEvent{Code: uv.KeyRight, Mod: uv.ModCtrl | uv.ModShift})
	eventually(t, "cat -v output", func() bool {
		return strings.Contains(s.screenText(), "^[[1;5D^[[D^[[1;6C")
	})
}
