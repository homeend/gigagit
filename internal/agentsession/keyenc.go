package agentsession

import (
	"strconv"

	uv "github.com/charmbracelet/ultraviolet"
)

// encodeModifiedKey encodes a special key that carries a modifier
// (ctrl+left, shift+home, ctrl+pgup, alt+f5, …) in xterm's CSI form,
// which every child — readline, Ink, Bubble Tea, vim — parses:
//
//	arrows / Home / End / F1–F4:  CSI 1 ; <m> <final>
//	Ins / Del / PgUp / PgDn / F5+: CSI <n> ; <m> ~
//	m = 1 + shift(1) + alt(2) + ctrl(4) + meta(8)
//
// The x/vt emulator's own SendKey knows only the UNMODIFIED specials and
// writes an empty string when a modifier is set, so without this every
// modified arrow the frontends map (`consoleSpecial`, the web key table)
// silently vanished. Unlike the plain arrows these forms do not depend on
// the child's application-cursor mode, so no emulator state is consulted.
// ok=false hands the key back to the emulator: unmodified keys, ctrl+letter,
// shift+tab, alt+rune — the cases it does encode correctly.
func encodeModifiedKey(k uv.KeyEvent) (string, bool) {
	kp, isPress := k.(uv.KeyPressEvent)
	if !isPress {
		return "", false
	}
	m := 1
	if kp.Mod&uv.ModShift != 0 {
		m++
	}
	if kp.Mod&uv.ModAlt != 0 {
		m += 2
	}
	if kp.Mod&uv.ModCtrl != 0 {
		m += 4
	}
	if kp.Mod&uv.ModMeta != 0 {
		m += 8
	}
	if m == 1 {
		return "", false
	}
	mod := strconv.Itoa(m)
	switch kp.Code {
	case uv.KeyUp:
		return "\x1b[1;" + mod + "A", true
	case uv.KeyDown:
		return "\x1b[1;" + mod + "B", true
	case uv.KeyRight:
		return "\x1b[1;" + mod + "C", true
	case uv.KeyLeft:
		return "\x1b[1;" + mod + "D", true
	case uv.KeyHome:
		return "\x1b[1;" + mod + "H", true
	case uv.KeyEnd:
		return "\x1b[1;" + mod + "F", true
	case uv.KeyF1:
		return "\x1b[1;" + mod + "P", true
	case uv.KeyF2:
		return "\x1b[1;" + mod + "Q", true
	case uv.KeyF3:
		return "\x1b[1;" + mod + "R", true
	case uv.KeyF4:
		return "\x1b[1;" + mod + "S", true
	}
	var n string
	switch kp.Code {
	case uv.KeyInsert:
		n = "2"
	case uv.KeyDelete:
		n = "3"
	case uv.KeyPgUp:
		n = "5"
	case uv.KeyPgDown:
		n = "6"
	case uv.KeyF5:
		n = "15"
	case uv.KeyF6:
		n = "17"
	case uv.KeyF7:
		n = "18"
	case uv.KeyF8:
		n = "19"
	case uv.KeyF9:
		n = "20"
	case uv.KeyF10:
		n = "21"
	case uv.KeyF11:
		n = "23"
	case uv.KeyF12:
		n = "24"
	default:
		return "", false
	}
	return "\x1b[" + n + ";" + mod + "~", true
}
