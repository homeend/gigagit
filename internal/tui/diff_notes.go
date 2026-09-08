package tui

import (
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// Review-note display rows. A resolved note becomes synthetic display rows
// appended AFTER its anchored line's content rows: the summary (◆ author:
// summary), one row per line of the optional rationale, and the same again per
// reply, indented. They live only in v.disp — v.lines, the change blocks and
// the shared textdiff rows are untouched, so the cursor, n/p, wrap and the
// fold machinery keep working on the same logical stream as before.
//
// Summaries and rationales are free text an agent may have written, so the row
// count relayout computes must not depend on what is inside them: a rationale
// is SPLIT on newlines here (one row each, so it reads naturally), and every
// row is sanitizeLine'd at render time — a stray \n, \t or bare \r can then
// never draw more physical rows than relayout accounted for.

// noteLine is ONE display row of a note (not one note): the pointer on dRow
// names the row, while id/rootID name the note it came from so E/R/Delete can
// find it again.
type noteLine struct {
	id     string // the note (root or reply) this row shows
	rootID string // the thread's root id (== id on a root row)
	depth  int    // 0 = root, 1 = reply (indent = 2*depth)
	text   string // the whole row, already assembled
	stale  bool   // the anchor text is gone: render dim
	agent  bool   // this ROW's own note is agent-written (the `a` layer filter)
}

// noteRowIndex maps logical line index → its note rows, and fold line index →
// "a note hides under this fold". While the agent layer is off, every ROW is
// filtered by its own note's source: an agent reply under a user root goes,
// a user reply under an agent root stays (keeping its indentation). User notes
// always render (hunk's policy). A thread with nothing left to show marks no
// fold either — a marker for something you cannot reveal is a lie.
func (v *diffView) noteRowIndex() (map[int][]noteLine, map[int]bool) {
	if len(v.notes) == 0 {
		return nil, nil
	}
	byLine := map[int][]noteLine{}
	foldMark := map[int]bool{}
	for _, r := range v.notes {
		rows := noteLinesOf(r)
		if v.hideAgent {
			rows = dropAgentRows(rows)
		}
		if len(rows) == 0 {
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
		byLine[li] = append(byLine[li], rows...)
	}
	return byLine, foldMark
}

// dropAgentRows keeps the rows the hidden agent layer still shows. It filters
// per ROW, not per thread, so the two mixed-authorship cases both behave.
func dropAgentRows(rows []noteLine) []noteLine {
	out := rows[:0:0]
	for _, nl := range rows {
		if !nl.agent {
			out = append(out, nl)
		}
	}
	return out
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

// noteRowsFor is one note's own rows: the summary (always exactly one row —
// any newline inside it is flattened by the renderer's sanitizeLine), then one
// row per line of the rationale.
func noteRowsFor(r domain.ResolvedNote, rootID string, depth int) []noteLine {
	stale := r.Status == model.NoteStale
	agent := r.Note.Source == model.NoteSourceAgent
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
	rows := []noteLine{{id: r.Note.ID, rootID: rootID, depth: depth, text: head, stale: stale, agent: agent}}
	if r.Note.Rationale != "" {
		for _, ln := range strings.Split(r.Note.Rationale, "\n") {
			rows = append(rows, noteLine{id: r.Note.ID, rootID: rootID, depth: depth,
				text: "  " + ln, stale: stale, agent: agent})
		}
	}
	return rows
}
