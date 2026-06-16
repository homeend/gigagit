// Package hunkpick is a pure, dependency-free model for picking the resolution
// of a two-version document region by region (and line by line within a
// region). The conflict resolver is its first consumer; hunk staging reuses the
// same Doc/Block model with a different constructor.
package hunkpick

import "bytes"

// Side identifies one of the two pickable versions.
type Side int

const (
	Current Side = iota // git stage :2: (its "ours")
	Incoming            // git stage :3: (its "theirs")
)

// Mode is how a block resolves.
type Mode int

const (
	Undecided Mode = iota
	TakeCurrent
	TakeIncoming
	LineByLine
)

// Pick is one line chosen in line-by-line mode: a side and an index into that
// side's lines.
type Pick struct {
	Side Side
	Line int
}

// Block is one decidable region: the two candidate versions plus the decision.
type Block struct {
	Current  []string
	Incoming []string
	Mode     Mode
	Picks    []Pick // ordered; only meaningful when Mode == LineByLine
}

// lines returns the slice for side s.
func (b *Block) lines(s Side) []string {
	if s == Current {
		return b.Current
	}
	return b.Incoming
}

// Picked reports whether (s, line) is currently in the ordered pick list.
func (b *Block) Picked(s Side, line int) bool {
	for _, p := range b.Picks {
		if p.Side == s && p.Line == line {
			return true
		}
	}
	return false
}

// ToggleLine appends (s, line) to the ordered picks if absent, or removes it if
// present (the remaining picks keep their order). Pure — the caller is
// responsible for having set Mode == LineByLine.
func (b *Block) ToggleLine(s Side, line int) {
	for i, p := range b.Picks {
		if p.Side == s && p.Line == line {
			b.Picks = append(b.Picks[:i], b.Picks[i+1:]...)
			return
		}
	}
	b.Picks = append(b.Picks, Pick{Side: s, Line: line})
}

// resolved appends this block's resolved lines to out, or reports ok=false when
// the block is still Undecided.
func (b *Block) resolved(out []string) ([]string, bool) {
	switch b.Mode {
	case TakeCurrent:
		return append(out, b.Current...), true
	case TakeIncoming:
		return append(out, b.Incoming...), true
	case LineByLine:
		for _, p := range b.Picks {
			ls := b.lines(p.Side)
			if p.Line >= 0 && p.Line < len(ls) {
				out = append(out, ls[p.Line])
			}
		}
		return out, true
	default:
		return out, false
	}
}

// Item is exactly one of: literal passthrough text (Literal != nil), or a
// decidable block (Block != nil).
type Item struct {
	Literal []string
	Block   *Block
}

// Doc is the whole file as an ordered mix of passthrough text and blocks.
type Doc struct {
	Items        []Item
	FinalNewline bool // whether the source file ended with a newline
}

// Blocks returns the decidable blocks in file order (pointers into Items).
func (d *Doc) Blocks() []*Block {
	var bs []*Block
	for _, it := range d.Items {
		if it.Block != nil {
			bs = append(bs, it.Block)
		}
	}
	return bs
}

// Pending counts blocks still Undecided.
func (d *Doc) Pending() int {
	n := 0
	for _, b := range d.Blocks() {
		if b.Mode == Undecided {
			n++
		}
	}
	return n
}

// SetAll sets every block's mode (used for take-all-current / -incoming).
func (d *Doc) SetAll(m Mode) {
	for _, b := range d.Blocks() {
		b.Mode = m
	}
}

// Resolved assembles the whole document. ok=false if any block is Undecided.
func (d *Doc) Resolved() (out []byte, ok bool) {
	var lines []string
	for _, it := range d.Items {
		if it.Block == nil {
			lines = append(lines, it.Literal...)
			continue
		}
		var done bool
		lines, done = it.Block.resolved(lines)
		if !done {
			return nil, false
		}
	}
	buf := bytes.Join(toBytes(lines), []byte("\n"))
	if d.FinalNewline && len(lines) > 0 {
		buf = append(buf, '\n')
	}
	return buf, true
}

func toBytes(ss []string) [][]byte {
	out := make([][]byte, len(ss))
	for i, s := range ss {
		out[i] = []byte(s)
	}
	return out
}
