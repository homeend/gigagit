package agentsession

// oscFilter strips non-ASCII bytes from the payload of string escape
// sequences (OSC, DCS, APC, PM, SOS) before they reach the emulator.
//
// x/ansi's parser treats byte 0x9C as the C1 String Terminator inside those
// strings even when it is the middle of a UTF-8 character, so a window title
// like "✳ Claude Code" (E2 9C B3 …) ends early and the rest is printed at the
// cursor — Claude Code sets such titles on every spinner frame. The payloads
// are titles, hyperlink targets and base64 clipboard data, none of which the
// console shows, so dropping their non-ASCII bytes costs nothing visible;
// UTF-8 in ordinary screen text is untouched. State carries across calls, so
// a sequence split between two reads is filtered the same.
type oscFilter struct {
	state filterState
	out   []byte
}

type filterState uint8

const (
	fGround    filterState = iota
	fEsc                   // saw ESC in ground
	fString                // inside an OSC/DCS/APC/PM/SOS payload
	fStringEsc             // saw ESC inside a payload (ESC \ ends it)
)

// filter returns p with string-payload bytes >= 0x80 removed. The returned
// slice is reused by the next call.
func (f *oscFilter) filter(p []byte) []byte {
	f.out = f.out[:0]
	for _, b := range p {
		switch f.state {
		case fGround:
			if b == 0x1B {
				f.state = fEsc
			}
		case fEsc:
			switch b {
			case ']', 'P', '_', '^', 'X': // OSC, DCS, APC, PM, SOS
				f.state = fString
			case 0x1B:
				// ESC ESC: still at an escape
			default:
				f.state = fGround
			}
		case fString:
			switch {
			case b == 0x07, b == 0x18, b == 0x1A: // BEL ends; CAN/SUB cancel
				f.state = fGround
			case b == 0x1B:
				f.state = fStringEsc
			case b >= 0x80:
				continue // the byte x/ansi could mistake for a C1 terminator
			}
		case fStringEsc:
			switch b {
			case '\\': // ST
				f.state = fGround
			case ']', 'P', '_', '^', 'X': // ESC ends the string and opens a new one
				f.state = fString
			case 0x1B:
				f.state = fEsc
			default:
				f.state = fGround
			}
		}
		f.out = append(f.out, b)
	}
	return f.out
}
