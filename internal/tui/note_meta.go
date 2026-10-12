package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/syntax"
)

// The TUI's rendering of note meta (spec 2026-10-12-note-meta §6): every
// surface paints a notemeta.Resolved through ONE mechanism — the meta
// pseudo-classes below ride a syntax.Class mask (like the markdown classes),
// so a badge is coloured the same on a note box, a send-panel row or a file
// row. Agent keys and values are data: shown verbatim, never translated.

// metaColours is the declaration's colour vocabulary in class order.
var metaColours = [7]string{"red", "orange", "yellow", "green", "blue", "purple", "grey"}

// The meta pseudo-classes: the text style's seven, then the filled style's.
const (
	metaClassStart syntax.Class = 200
	metaClassEnd   syntax.Class = metaClassStart + 14
)

// metaClass is colour in style as a mask class; 0 (plain) for a colour
// outside the vocabulary.
func metaClass(color, style string) syntax.Class {
	for i, c := range metaColours {
		if c == color {
			if style == notemeta.StyleFilled {
				return metaClassStart + 7 + syntax.Class(i)
			}
			return metaClassStart + syntax.Class(i)
		}
	}
	return 0
}

// metaStyle paints c over base: a text class colours the foreground, a
// filled one the background with the theme's ground as its text.
func (s *styles) metaStyle(base lipgloss.Style, c syntax.Class) lipgloss.Style {
	i := int(c - metaClassStart)
	if i >= 7 {
		return base.Background(s.metaFg[i-7]).Foreground(s.metaFillFg)
	}
	return base.Foreground(s.metaFg[i])
}

// maskRun builds a run of text with the mask that colours it, one class per
// rune.
type maskRun struct {
	b   strings.Builder
	cls []syntax.Class
}

func (r *maskRun) add(s string, c syntax.Class) {
	r.b.WriteString(s)
	for range []rune(s) {
		r.cls = append(r.cls, c)
	}
}

// badgeValue is what a badge says: the value verbatim (data), except the
// built-in status values, which are gg's own words.
func badgeValue(b notemeta.Badge) string { return metaWord(b.Key, b.Value) }

// metaWord is value as shown for key: verbatim (sanitized) data, except the
// built-in status values.
func metaWord(key, value string) string {
	if key == notemeta.StatusKey {
		switch value {
		case "agreed":
			return i18n.T("agreed")
		case "disagreed":
			return i18n.T("disagreed")
		case "deferred":
			return i18n.T("deferred")
		}
	}
	return sanitizeLine(value)
}

// badgeRun is a note's badges then its audience marker (spec §6.1), with the
// mask that colours them: a filled chip carries one column of fill each
// side and sits one space from its neighbours, two text badges sit two
// apart, the marker two. "" when the note shows
// neither.
func badgeRun(res notemeta.Resolved) (string, []syntax.Class) {
	var r maskRun
	for i, bd := range res.Badges {
		switch {
		case i == 0:
		case bd.Style == notemeta.StyleFilled || res.Badges[i-1].Style == notemeta.StyleFilled:
			r.add(" ", 0) // a chip's own fill already spaces it
		default:
			r.add("  ", 0)
		}
		v := badgeValue(bd)
		if bd.Glyph != "" {
			v = bd.Glyph + " " + v
		}
		if bd.Style == notemeta.StyleFilled {
			v = " " + v + " "
		}
		r.add(v, metaClass(bd.Color, bd.Style))
	}
	if m := audienceMarker(res.Audience); m != "" {
		if r.b.Len() > 0 {
			r.add("  ", 0)
		}
		r.add(m, 0)
	}
	return r.b.String(), r.cls
}

// audienceMarker is who a note is for (spec §3.3): the PR author, or the
// reader alone. "" for no audience.
func audienceMarker(a string) string {
	switch a {
	case "author":
		return i18n.T("→ author")
	case "reviewer":
		return i18n.T("for you")
	}
	return ""
}

// metaLineTexts are a note's line rows: "label: value", one per entry.
func metaLineTexts(res notemeta.Resolved) []string {
	out := make([]string, 0, len(res.Lines))
	for _, l := range res.Lines {
		out = append(out, sanitizeLine(l.Label+": "+l.Value))
	}
	return out
}

// metaDetailTexts are a note's details rows (spec §6.1): its stored-only
// meta, the meta added onto it (the author's value it replaced, who added
// it) and the problems its declaration has. nil when there are none.
func metaDetailTexts(res notemeta.Resolved) []string {
	var out []string
	if len(res.Stored) > 0 {
		parts := make([]string, len(res.Stored))
		for i, kv := range res.Stored {
			parts[i] = sanitizeLine(kv.Key + ": " + kv.Value)
		}
		out = append(out, i18n.T("meta: %s", strings.Join(parts, " · ")))
	}
	if len(res.Added) > 0 {
		parts := make([]string, len(res.Added))
		for i, c := range res.Added {
			v := c.To
			if c.From != "" {
				v = c.From + " → " + c.To
			}
			if c.By != "" {
				v += " (" + c.By + ")"
			}
			parts[i] = sanitizeLine(c.Key + ": " + v)
		}
		out = append(out, i18n.T("added: %s", strings.Join(parts, " · ")))
	}
	for _, p := range res.Problems {
		out = append(out, i18n.T("display problem: %s", sanitizeLine(string(p))))
	}
	return out
}

// metaCountsText is a set of notes' counts in plain words (spec §6.2):
// "3 issue · 8 good · 5 for author · 9 for you". "" for none.
func metaCountsText(c notemeta.Counts) string {
	var parts []string
	for _, g := range c.Groups {
		for _, v := range g.Values {
			parts = append(parts, strconv.Itoa(v.N)+" "+metaWord(g.Key, v.Value))
		}
	}
	if c.Author > 0 {
		parts = append(parts, i18n.T("%d for author", c.Author))
	}
	if c.Reviewer > 0 {
		parts = append(parts, i18n.T("%d for you", c.Reviewer))
	}
	return strings.Join(parts, " · ")
}

// metaCountsRun is the compact form a file row carries: "issue 3 good 8",
// each number in its value's colour.
func metaCountsRun(c notemeta.Counts) (string, []syntax.Class) {
	var r maskRun
	for _, g := range c.Groups {
		for _, v := range g.Values {
			if r.b.Len() > 0 {
				r.add(" ", 0)
			}
			r.add(metaWord(g.Key, v.Value)+" ", 0)
			r.add(strconv.Itoa(v.N), metaClass(v.Color, notemeta.StyleText))
		}
	}
	return r.b.String(), r.cls
}

// metaTitleSep separates a badge run from the text after it (a box title,
// a collapsed row): "issue  high  → author · review · …".
const metaTitleSep = " · "

// minPathCols is the narrowest a title's path is elided to before today's
// parts start to go: "…/x.go" still names the file.
const minPathCols = 8

// fitNoteTitle fits a box title into room columns (spec §6.1): whole when it
// fits; else the path loses its middle (never its file name); else the
// words before the path are cut, then the path's tail; the badges go last. badges and text are joined by
// metaTitleSep when both stay.
func fitNoteTitle(badges string, cls []syntax.Class, text, path string, room int) (string, []syntax.Class, string) {
	if room <= 0 {
		return "", nil, ""
	}
	bw := lipgloss.Width(badges)
	gap := 0
	if badges != "" && text != "" {
		gap = lipgloss.Width(metaTitleSep)
	}
	if bw+gap+lipgloss.Width(text) <= room {
		return badges, cls, text
	}
	textRoom := room - bw - gap
	if path != "" && textRoom > 0 {
		if before, after, ok := strings.Cut(text, path); ok {
			fixed := lipgloss.Width(before) + lipgloss.Width(after)
			if pw := textRoom - fixed; pw >= minPathCols {
				return badges, cls, before + elidePath(path, pw) + after
			}
			// The file and its line outrank the words before them: those
			// are cut first, the path's tail only when nothing else is left.
			tail := elidePath(path, minPathCols) + after
			if rest := textRoom - lipgloss.Width(tail); rest >= 0 {
				return badges, cls, truncate(before, rest) + tail
			}
			text = tail
		}
	}
	if textRoom >= 1 {
		return badges, cls, truncate(text, textRoom)
	}
	// Today's parts are gone; the badges take the room, cut at their end.
	r := []rune(badges)
	w, cut := 0, 0
	for i, c := range r {
		cw := lipgloss.Width(string(c))
		if w+cw > room {
			break
		}
		w += cw
		cut = i + 1
	}
	return string(r[:cut]), cls[:min(cut, len(cls))], ""
}
