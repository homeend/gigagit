package tui

import (
	"errors"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

// The overview split's diff rows (spec 2026-10-11-overview-split-diff §4): a
// file's whole aligned diff as ONE column — a run's deleted lines, then its
// added ones, an unchanged line once (gg web's splitdiff.js, unifiedRows).
// Pure: no Model, no keys. The cached diff's rows are shared read-only
// (domain.Diff), so every row here holds its own copy of the text.

type diffRowKind uint8

const (
	diffSame diffRowKind = iota
	diffDel
	diffAdd
	diffFold // ⋯ N unchanged lines (f's changed-only view)
)

// diffRow is one row of the column. old / nu are the line's numbers on each
// side (0 = none); text is what is drawn (tabs expanded, controls shown),
// raw the source a copy takes; cls and words run per display rune (words
// marks a changed pair's changed words; nil = none). A fold stands for
// hidden unchanged rows. at is the row's index in the unfolded list (a
// fold's: its first hidden row) — what a fold is opened by.
type diffRow struct {
	kind    diffRowKind
	old, nu int
	text    string
	raw     string
	cls     []syntax.Class
	words   []emphLevel
	hidden  int
	at      int
}

// unifiedDiffRows is d's rows as one column.
func unifiedDiffRows(d domain.Diff) []diffRow {
	src := d.Result.Rows
	var out []diffRow
	push := func(kind diffRowKind, old, nu int, s string, spans []textdiff.Span, side [][]syntax.Tok, no int) {
		toks := tokAt(side, no)
		disp, emph, cls := sanitizeCell(s, spans, toks)
		r := diffRow{kind: kind, old: old, nu: nu, text: string(disp), raw: s, at: len(out)}
		if toks != nil {
			r.cls = cls
		}
		if len(spans) > 0 {
			r.words = emph
		}
		out = append(out, r)
	}
	for k := 0; k < len(src); {
		if r := src[k]; r.Kind == textdiff.Same {
			push(diffSame, r.LeftNo, r.RightNo, r.Right, nil, d.NewTok, r.RightNo)
			k++
			continue
		}
		j := k
		for j < len(src) && src[j].Kind != textdiff.Same {
			j++
		}
		for x := k; x < j; x++ {
			if r := src[x]; r.Kind != textdiff.Add {
				push(diffDel, r.LeftNo, 0, r.Left, r.LeftSpans, d.OldTok, r.LeftNo)
			}
		}
		for x := k; x < j; x++ {
			if r := src[x]; r.Kind != textdiff.Del {
				push(diffAdd, 0, r.RightNo, r.Right, r.RightSpans, d.NewTok, r.RightNo)
			}
		}
		k = j
	}
	return out
}

// rowOfNew is the row holding new line line, -1 when none does (a folded
// line has none).
func rowOfNew(rows []diffRow, line int) int {
	for i, r := range rows {
		if r.nu == line && r.kind != diffFold {
			return i
		}
	}
	return -1
}

// rowOfOld is the row holding old line line, -1 when none does.
func rowOfOld(rows []diffRow, line int) int {
	for i, r := range rows {
		if r.old == line && r.kind != diffFold {
			return i
		}
	}
	return -1
}

// rowNewLine is the new line row i stands for: its own; a deleted row's or
// a fold's, the next new line below it, else the last one above; 0 = none
// (a deleted file).
func rowNewLine(rows []diffRow, i int) int {
	if i < 0 || i >= len(rows) {
		return 0
	}
	for k := i; k < len(rows); k++ {
		if rows[k].nu > 0 && rows[k].kind != diffFold {
			return rows[k].nu
		}
	}
	for k := i - 1; k >= 0; k-- {
		if rows[k].nu > 0 && rows[k].kind != diffFold {
			return rows[k].nu
		}
	}
	return 0
}

// foldDiffRows is f's changed-only view of all: a change keeps diffContext
// unchanged rows on each side, a keep(i) row (a band's, a remark's) shows
// alone, every other run of unchanged rows becomes one fold row — unless
// open names it (by its first row's at).
func foldDiffRows(all []diffRow, keep func(i int) bool, open map[int]bool) []diffRow {
	show := make([]bool, len(all))
	for i, r := range all {
		switch {
		case r.kind != diffSame:
			for k := max(i-diffContext, 0); k <= min(i+diffContext, len(all)-1); k++ {
				show[k] = true
			}
		case keep != nil && keep(i):
			show[i] = true
		}
	}
	var out []diffRow
	for i := 0; i < len(all); {
		if show[i] {
			out = append(out, all[i])
			i++
			continue
		}
		j := i
		for j < len(all) && !show[j] {
			j++
		}
		if open[all[i].at] {
			out = append(out, all[i:j]...)
		} else {
			out = append(out, diffRow{kind: diffFold, hidden: j - i, at: all[i].at})
		}
		i = j
	}
	return out
}

// diffNoticeText is what the column says instead of rows ("" = the rows):
// a gone review, a failed read, a binary or too large diff, or no change.
// permanent: the file was opened from a permanent overview (its commit's).
func diffNoticeText(res domain.ReviewFileDiff, err error, agent, permanent bool) string {
	switch {
	case errors.Is(err, domain.ErrOverviewNotFound):
		return i18n.T("the overview is gone — D shows the file")
	case errors.Is(err, domain.ErrReviewNotFound):
		return i18n.T("the review is gone — D shows the file")
	case err != nil:
		return i18n.T("could not read the diff — D shows the file")
	case res.Diff.Binary:
		return i18n.T("binary file — D shows the file")
	case res.Diff.TooLarge:
		return i18n.T("diff too large — D shows the file")
	case !res.Changed || !rowsChange(res.Diff.Result.Rows):
		if agent {
			return i18n.T("no changes in the working tree — D shows the file")
		}
		if permanent {
			return i18n.T("no change to this file in the overview's commit — D shows the file")
		}
		return i18n.T("no change to this file in the review — D shows the file")
	}
	return ""
}

// rowsChange reports whether any aligned row changed.
func rowsChange(rows []textdiff.Row) bool {
	for _, r := range rows {
		if r.Kind != textdiff.Same {
			return true
		}
	}
	return false
}

// diffTitle is the column's title in diff mode: the path (a rename's old
// path first) and the two sides.
func diffTitle(path string, res domain.ReviewFileDiff) string {
	name := path
	if res.OldPath != "" && res.OldPath != path {
		name = res.OldPath + " → " + path
	}
	return i18n.T("Diff %s (%s → %s)", name, res.Left, res.Right)
}

// sameText reports whether two files' lines are the same, non-empty text.
func sameText(a, b []string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
