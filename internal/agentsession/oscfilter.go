package agentsession

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

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
//
// It also records, from the unfiltered bytes, the last window title (OSC 0
// or 2) and the last OSC 9;4 progress state: agents announce working and
// idle there (agentstate's title rules). Only those payloads are buffered,
// the title capped at titleCap.
type oscFilter struct {
	state filterState
	out   []byte

	osc         oscKind // the OSC being read: unknown until its number ends
	num         []byte  // the OSC number read so far
	pay         []byte  // a title/progress payload read so far
	title       string  // the last complete title
	progress    int     // the last OSC 9;4 state (0–4)
	hasProgress bool    // an OSC 9;4 has been seen
	changed     bool    // title or progress committed since the reader last cleared it

	clipBuf     []byte // the OSC 52 payload read so far (capped at clipCap)
	clipLong    bool   // the payload being read passed clipCap
	clip        string // the last decoded clipboard write
	clipSeq     int    // clipboard writes committed (an over-cap one included)
	clipOver    bool   // the last write was over clipCap and dropped
	clipChanged bool   // a clipboard write committed since the reader last cleared it
}

type filterState uint8

const (
	fGround    filterState = iota
	fEsc                   // saw ESC in ground
	fString                // inside an OSC/DCS/APC/PM/SOS payload
	fStringEsc             // saw ESC inside a payload (ESC \ ends it)
)

type oscKind uint8

const (
	oscNone     oscKind = iota // not an OSC, or one gg does not record
	oscNumber                  // an OSC whose number is still being read
	oscTitle                   // OSC 0 / OSC 2
	oscProgress                // OSC 9 (a progress report only if it reads "4;<state>")
	oscClip                    // OSC 52: a clipboard write
)

// clipCap bounds an OSC 52 payload (base64 bytes); a longer one is dropped.
const clipCap = 1 << 20

// titleCap bounds a recorded title; longer payloads are cut on a rune.
const titleCap = 256

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
			case ']': // OSC
				f.state, f.osc, f.num, f.pay = fString, oscNumber, f.num[:0], f.pay[:0]
			case 'P', '_', '^', 'X': // DCS, APC, PM, SOS
				f.state, f.osc = fString, oscNone
			case 0x1B:
				// ESC ESC: still at an escape
			default:
				f.state = fGround
			}
		case fString:
			switch {
			case b == 0x07: // BEL ends
				f.commit()
				f.state = fGround
			case b == 0x18, b == 0x1A: // CAN/SUB cancel
				f.state, f.osc = fGround, oscNone
			case b == 0x1B:
				f.state = fStringEsc
			default:
				f.record(b)
				if b >= 0x80 {
					continue // the byte x/ansi could mistake for a C1 terminator
				}
			}
		case fStringEsc:
			switch b {
			case '\\': // ST
				f.commit()
				f.state = fGround
			case ']': // ESC ends the string and opens a new one
				f.state, f.osc, f.num, f.pay = fString, oscNumber, f.num[:0], f.pay[:0]
			case 'P', '_', '^', 'X':
				f.state, f.osc = fString, oscNone
			case 0x1B:
				f.state, f.osc = fEsc, oscNone
			default:
				f.state, f.osc = fGround, oscNone
			}
		}
		f.out = append(f.out, b)
	}
	return f.out
}

// record takes one payload byte of the current string.
func (f *oscFilter) record(b byte) {
	switch f.osc {
	case oscNumber:
		if b == ';' {
			switch string(f.num) {
			case "0", "2":
				f.osc = oscTitle
			case "9":
				f.osc = oscProgress
			case "52":
				f.osc, f.clipBuf, f.clipLong = oscClip, f.clipBuf[:0], false
			default:
				f.osc = oscNone
			}
			return
		}
		if b < '0' || b > '9' || len(f.num) >= 4 {
			f.osc = oscNone
			return
		}
		f.num = append(f.num, b)
	case oscTitle:
		if len(f.pay) < titleCap+utf8.UTFMax {
			f.pay = append(f.pay, b)
		}
	case oscProgress:
		if len(f.pay) < 16 {
			f.pay = append(f.pay, b)
		}
	case oscClip:
		if len(f.clipBuf) < clipCap {
			f.clipBuf = append(f.clipBuf, b)
		} else {
			f.clipLong = true
		}
	}
}

// commit stores a complete title or progress report.
func (f *oscFilter) commit() {
	switch f.osc {
	case oscTitle:
		t := f.pay
		if len(t) > titleCap {
			t = t[:titleCap]
		}
		f.title, f.changed = strings.ToValidUTF8(string(t), ""), true
	case oscNumber: // "ESC ] 0 BEL": a number with no payload
		if n := string(f.num); n == "0" || n == "2" {
			f.title, f.changed = "", true
		}
	case oscProgress:
		rest, ok := strings.CutPrefix(string(f.pay), "4;")
		if !ok {
			break
		}
		st, _, _ := strings.Cut(rest, ";")
		if len(st) == 1 && st[0] >= '0' && st[0] <= '4' {
			f.progress, f.hasProgress, f.changed = int(st[0]-'0'), true, true
		}
	case oscClip:
		f.commitClip()
	}
	f.osc = oscNone
}

// commitClip stores an OSC 52 write: "<selection>;<base64>". A read request
// ("?") and undecodable data are ignored; an over-cap payload is recorded as
// dropped so the frontend can say so.
func (f *oscFilter) commitClip() {
	if f.clipLong {
		f.clip, f.clipOver = "", true
		f.clipSeq++
		f.clipChanged = true
		return
	}
	_, data, ok := strings.Cut(string(f.clipBuf), ";")
	if !ok || data == "?" {
		return
	}
	dec, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return
	}
	f.clip, f.clipOver = string(dec), false
	f.clipSeq++
	f.clipChanged = true
}
