package domain

import (
	"errors"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
)

// ConsoleKey is one keystroke as the web page sends it: an allowlisted
// name, a modifier mask, and the text for "char". The TUI maps Bubble Tea
// keys to the same emulator events (console_keys.go there); both frontends
// therefore give the child identical bytes for the same key.
type ConsoleKey struct {
	K    string `json:"k"`
	Mod  int    `json:"mod"`
	Text string `json:"text"`
}

// Modifier bits of ConsoleKey.Mod.
const (
	ModShift = 1
	ModCtrl  = 2
	ModAlt   = 4
	modAll   = ModShift | ModCtrl | ModAlt
)

// ConsoleInput is a decoded ConsoleKey: literal text for SendText, or a key
// event for SendKey (the emulator encodes it for the child's modes).
type ConsoleInput struct {
	Text  string
	Key   SessionKey
	IsKey bool
}

var consoleKeyNames = map[string]rune{
	"enter": uv.KeyEnter, "tab": uv.KeyTab, "esc": uv.KeyEscape, "backspace": uv.KeyBackspace,
	"delete": uv.KeyDelete, "insert": uv.KeyInsert,
	"up": uv.KeyUp, "down": uv.KeyDown, "left": uv.KeyLeft, "right": uv.KeyRight,
	"home": uv.KeyHome, "end": uv.KeyEnd, "pgup": uv.KeyPgUp, "pgdn": uv.KeyPgDown,
	"f1": uv.KeyF1, "f2": uv.KeyF2, "f3": uv.KeyF3, "f4": uv.KeyF4, "f5": uv.KeyF5, "f6": uv.KeyF6,
	"f7": uv.KeyF7, "f8": uv.KeyF8, "f9": uv.KeyF9, "f10": uv.KeyF10, "f11": uv.KeyF11, "f12": uv.KeyF12,
}

func uvMod(mod int) uv.KeyMod {
	var m uv.KeyMod
	if mod&ModShift != 0 {
		m |= uv.ModShift
	}
	if mod&ModCtrl != 0 {
		m |= uv.ModCtrl
	}
	if mod&ModAlt != 0 {
		m |= uv.ModAlt
	}
	return m
}

// ConsoleKeyEvent decodes one wire key. Plain text (no ctrl/alt) is sent as
// text so an IME commit or a burst of runes stays one write; a ctrl/alt
// letter is a key event with exactly one rune.
func ConsoleKeyEvent(k ConsoleKey) (ConsoleInput, error) {
	if k.Mod&^modAll != 0 {
		return ConsoleInput{}, errors.New("unknown modifier")
	}
	if k.K == "char" {
		if k.Text == "" {
			return ConsoleInput{}, errors.New("char without text")
		}
		if k.Mod&(ModCtrl|ModAlt) == 0 {
			return ConsoleInput{Text: k.Text}, nil
		}
		if utf8.RuneCountInString(k.Text) != 1 {
			return ConsoleInput{}, errors.New("a modified char is one rune")
		}
		r, _ := utf8.DecodeRuneInString(k.Text)
		return ConsoleInput{IsKey: true, Key: uv.KeyPressEvent{Code: r, Mod: uvMod(k.Mod)}}, nil
	}
	code, ok := consoleKeyNames[k.K]
	if !ok {
		return ConsoleInput{}, errors.New("unknown key " + k.K)
	}
	return ConsoleInput{IsKey: true, Key: uv.KeyPressEvent{Code: code, Mod: uvMod(k.Mod)}}, nil
}

// ClampConsoleSize bounds a viewer's cols×rows so a stray request can never
// allocate a huge emulator or a useless one.
func ClampConsoleSize(cols, rows int) (int, int) {
	return min(max(cols, 20), 500), min(max(rows, 5), 300)
}
