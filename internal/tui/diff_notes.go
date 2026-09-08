package tui

import (
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// Review-note display rows. A resolved note becomes one to three synthetic
// display rows appended AFTER its anchored line's content rows: the summary
// (◆ author: summary), an optional rationale row, and one such pair per
// reply, indented. They live only in v.disp — v.lines, the change blocks and
// the shared textdiff rows are untouched, so the cursor, n/p, wrap and the
// fold machinery keep working on the same logical stream as before.

// noteLine is ONE display row of a note (not one note): the pointer on dRow
// names the row, while id/rootID name the note it came from so E/R/Delete can
// find it again.
type noteLine struct {
	id     string // the note (root or reply) this row shows
	rootID string // the thread's root id (== id on a root row)
	depth  int    // 0 = root, 1 = reply (indent = 2*depth)
	text   string // the whole row, already assembled
	stale  bool   // the anchor text is gone: render dim
}

// noteRowIndex maps logical line index → its note rows, and fold line index →
// "a note hides under this fold". Agent-sourced notes are skipped entirely
// while the agent layer is off; user notes always render (hunk's policy).
func (v *diffView) noteRowIndex() (map[int][]noteLine, map[int]bool) {
	if len(v.notes) == 0 {
		return nil, nil
	}
	byLine := map[int][]noteLine{}
	foldMark := map[int]bool{}
	for _, r := range v.notes {
		if v.hideAgent && r.Note.Source == model.NoteSourceAgent {
			continue
		}
		li, visible := v.noteAnchorLine(r)
		if li < 0 {
			continue // the anchor is not in this view at all
		}
		if !visible {
			foldMark[li] = true // folded away: mark the fold rule instead
			continue
		}
		byLine[li] = append(byLine[li], noteLinesOf(r)...)
	}
	return byLine, foldMark
}

// noteAnchorLine finds the logical line a note hangs off: the line carrying
// the END of its range on its side (§4.4 phase-2 hunks anchor at the range
// end). visible=false means the number exists in the file but is folded away,
// and the returned index is the FOLD entry that hides it. (-1, false) means
// the number is not in this view at all.
func (v *diffView) noteAnchorLine(r domain.ResolvedNote) (int, bool) {
	no := r.Range[1]
	if no <= 0 {
		return -1, false
	}
	old := r.Note.Side == model.NoteSideOld
	numOf := func(ln textdiff.Line) int {
		if old {
			return ln.Row.LeftNo
		}
		return ln.Row.RightNo
	}
	prev := 0
	for i, ln := range v.lines {
		if ln.Fold > 0 {
			// The hidden run spans (prev, next): find the next real number.
			next := 0
			for j := i + 1; j < len(v.lines); j++ {
				if v.lines[j].Fold == 0 && numOf(v.lines[j]) > 0 {
					next = numOf(v.lines[j])
					break
				}
			}
			if no > prev && (next == 0 || no < next) {
				return i, false
			}
			continue
		}
		if numOf(ln) == no {
			return i, true
		}
		if n := numOf(ln); n > 0 {
			prev = n
		}
	}
	return -1, false
}

// noteLinesOf flattens one resolved note (and its replies) into display rows.
func noteLinesOf(r domain.ResolvedNote) []noteLine {
	rows := noteRowsFor(r, r.Note.ID, 0)
	for _, rep := range r.Replies {
		rows = append(rows, noteRowsFor(rep, r.Note.ID, 1)...)
	}
	return rows
}

// noteRowsFor is one note's own rows: the summary, then the rationale.
func noteRowsFor(r domain.ResolvedNote, rootID string, depth int) []noteLine {
	stale := r.Status == model.NoteStale
	head := "◆ "
	if depth > 0 {
		head = "↳ "
	}
	if r.Note.Author != "" {
		head += r.Note.Author + ": "
	}
	head += r.Note.Summary
	if stale {
		head += " " + i18n.T("(stale)")
	}
	rows := []noteLine{{id: r.Note.ID, rootID: rootID, depth: depth, text: head, stale: stale}}
	if r.Note.Rationale != "" {
		rows = append(rows, noteLine{id: r.Note.ID, rootID: rootID, depth: depth,
			text: "  " + r.Note.Rationale, stale: stale})
	}
	return rows
}
