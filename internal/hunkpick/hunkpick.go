// Package hunkpick is a pure, dependency-free model for picking the resolution
// of a two-version document region by region (and line by line within a
// region). The conflict resolver is its first consumer; hunk staging reuses the
// same Doc/Block model with a different constructor.
package hunkpick

import "bytes"

// Side identifies one of the two pickable versions.
type Side int

const (
	Current  Side = iota // git stage :2: (its "ours")
	Incoming             // git stage :3: (its "theirs")
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

// Skip resolves the block with no lines from either side — the region is
// decided (Pending drops) and contributes nothing to the result. It is the
// state a pick-then-unpick leaves behind, reachable in one step.
func (b *Block) Skip() {
	b.Mode, b.Picks = LineByLine, nil
}

// Skipped reports the decided-with-nothing state Skip produces.
func (b *Block) Skipped() bool {
	return b.Mode == LineByLine && len(b.Picks) == 0
}

// Unskip returns a skipped block to Undecided (a no-op otherwise).
func (b *Block) Unskip() {
	if b.Skipped() {
		b.Mode, b.Picks = Undecided, nil
	}
}

// EnsurePicks converts a block to the line-pick representation: a legacy
// whole-side mode materializes as that side's full ordered picks, Undecided
// becomes an empty (touched) pick list. Already-LineByLine blocks are
// untouched.
func (b *Block) EnsurePicks() {
	switch b.Mode {
	case TakeCurrent:
		b.Picks = fullPicks(Current, len(b.Current))
	case TakeIncoming:
		b.Picks = fullPicks(Incoming, len(b.Incoming))
	case LineByLine:
		return
	default:
		b.Picks = nil
	}
	b.Mode = LineByLine
}

func fullPicks(s Side, n int) []Pick {
	ps := make([]Pick, 0, n)
	for i := 0; i < n; i++ {
		ps = append(ps, Pick{Side: s, Line: i})
	}
	return ps
}

// SideState reports whether all/any of side s's lines are picked, reading
// legacy whole-side modes as full picks of that side. A zero-line side is
// never all (nor any) picked.
func (b *Block) SideState(s Side) (all, any bool) {
	n := len(b.lines(s))
	if n == 0 {
		return false, false
	}
	switch b.Mode {
	case TakeCurrent:
		return s == Current, s == Current
	case TakeIncoming:
		return s == Incoming, s == Incoming
	case LineByLine:
		cnt := 0
		for _, p := range b.Picks {
			if p.Side == s && p.Line >= 0 && p.Line < n {
				cnt++
			}
		}
		return cnt == n, cnt > 0
	default:
		return false, false
	}
}

// LinePicked reports whether side s's line is in the result, reading legacy
// whole-side modes as full picks of that side. A zero-line side is never picked.
func (b *Block) LinePicked(s Side, line int) bool {
	if len(b.lines(s)) == 0 {
		return false
	}
	switch b.Mode {
	case TakeCurrent:
		return s == Current
	case TakeIncoming:
		return s == Incoming
	}
	return b.Picked(s, line)
}

// ToggleSide is the tri-state whole-side toggle: with every line of s picked
// it removes s's picks (the other side keeps its order); otherwise it appends
// s's missing lines top-to-bottom. A zero-line side is a no-op (the block is
// not even touched).
func (b *Block) ToggleSide(s Side) {
	if len(b.lines(s)) == 0 {
		return
	}
	b.EnsurePicks()
	if all, _ := b.SideState(s); all {
		kept := b.Picks[:0]
		for _, p := range b.Picks {
			if p.Side != s {
				kept = append(kept, p)
			}
		}
		b.Picks = kept
		return
	}
	for i := range b.lines(s) {
		if !b.Picked(s, i) {
			b.Picks = append(b.Picks, Pick{Side: s, Line: i})
		}
	}
}

// ResolvedLines is the exported per-block view of resolved (for previews):
// the block's picked lines in order, ok=false while Undecided.
func (b *Block) ResolvedLines() ([]string, bool) {
	return b.resolved(nil)
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

	// EOL is the terminator Resolved joins lines with; "" means "\n".
	// FromDiff sets "\r\n" when both inputs are consistently CRLF —
	// textdiff strips the \r from every line for alignment identity, so
	// the terminator must be re-applied at join time or a CRLF file
	// comes back entirely LF (the silent-rewrite bug the TUI H picker
	// shipped with). ParseConflict leaves it "" on purpose: its lines
	// keep their own \r (it splits on \n without trimming).
	EOL string
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

// ToggleSideAll is ToggleSide across the document: if every block that has
// s-lines is fully picked on s, it clears s from those blocks; otherwise it
// completes s on every block that has s-lines. Blocks without s-lines are
// left alone.
func (d *Doc) ToggleSideAll(s Side) {
	allFull, seen := true, false
	for _, b := range d.Blocks() {
		if len(b.lines(s)) == 0 {
			continue
		}
		seen = true
		if full, _ := b.SideState(s); !full {
			allFull = false
		}
	}
	if !seen {
		return
	}
	for _, b := range d.Blocks() {
		if len(b.lines(s)) == 0 {
			continue
		}
		full, _ := b.SideState(s)
		if allFull || !full {
			b.ToggleSide(s)
		}
	}
}

// SideStateAll aggregates SideState for the master checkbox: all = every
// block with s-lines has s fully picked (false when no block has s-lines);
// any = at least one s line picked anywhere.
func (d *Doc) SideStateAll(s Side) (all, any bool) {
	seen := false
	all = true
	for _, b := range d.Blocks() {
		if len(b.lines(s)) == 0 {
			continue
		}
		seen = true
		ba, bany := b.SideState(s)
		if !ba {
			all = false
		}
		if bany {
			any = true
		}
	}
	return all && seen, any
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
	sep := d.EOL
	if sep == "" {
		sep = "\n"
	}
	buf := bytes.Join(toBytes(lines), []byte(sep))
	if d.FinalNewline && len(lines) > 0 {
		buf = append(buf, sep...)
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
